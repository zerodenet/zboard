package zeroevent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFileSpoolReadBudgetBoundsLargePayloadsAndResumesAcrossSegments(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "zstd"}[compressed], func(t *testing.T) {
			cfg := testFileConfig(t)
			events := make([]Envelope, 6)
			for i := range events {
				events[i] = testEnvelope(string(rune('a'+i)), uint64(i+1))
				events[i].Payload = json.RawMessage(`{"large":"` + strings.Repeat("x", 2<<20) + `"}`)
			}
			write := writeRawTestSegment
			if compressed {
				write = writeCompressedTestSegment
			}
			write(t, cfg.Directory, 1, events[:5])
			write(t, cfg.Directory, 2, events[5:])
			spool, err := NewFileSpool(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = spool.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer spool.Close()
			seen := 0
			for seen < len(events) {
				batch, err := spool.ReadBatch(context.Background(), 2000)
				if err != nil {
					t.Fatal(err)
				}
				if len(batch.Events) == 0 || len(batch.Events) > 4 {
					t.Fatalf("unbounded or stalled read: %d", len(batch.Events))
				}
				for _, event := range batch.Events {
					if event.ID != events[seen].ID {
						t.Fatalf("lost or reordered event at %d: %s", seen, event.ID)
					}
					seen++
				}
				// An uncommitted window must replay identically.
				replay, err := spool.ReadBatch(context.Background(), 2000)
				if err != nil || len(replay.Events) != len(batch.Events) || replay.Next != batch.Next {
					t.Fatalf("replay=%+v err=%v", replay.Next, err)
				}
				if err := spool.Commit(context.Background(), batch.Next); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSemanticCompactionKeepsChangedCountersAndAccountingBoundaries(t *testing.T) {
	first := semanticTestEvent("first", "flow.updated", "core", "flow", 1)
	first.Payload = []byte(`{"flow_id":"flow","traffic":{"bytes_up":1,"bytes_down":2}}`)
	for name, change := range map[string]func(*Envelope){
		"changed_counters": func(e *Envelope) { e.Payload = []byte(`{"flow_id":"flow","traffic":{"bytes_up":2,"bytes_down":4}}`) },
		"principal":        func(e *Envelope) { e.PrincipalKey = "different" },
		"source":           func(e *Envelope) { e.SourceID = "different" },
		"config":           func(e *Envelope) { e.ConfigRevision++ },
	} {
		t.Run(name, func(t *testing.T) {
			second := first
			second.ID = "second"
			second.Sequence = 2
			change(&second)
			survivors, saved, _, _ := compactSemanticEvents([]Envelope{first, second}, DefaultConfig().Compaction)
			if len(survivors) != 2 || saved != 0 {
				t.Fatalf("discarded accounting facts: %d saved=%d", len(survivors), saved)
			}
		})
	}
}
