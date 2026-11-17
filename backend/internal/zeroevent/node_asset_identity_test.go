package zeroevent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestVersionOneAssetStampRoundTripAndLegacyUnknown(t *testing.T) {
	for _, tag := range []string{"", strings.Repeat("a", 64)} {
		event := semanticTestEvent("original", "flow.updated", strings.Repeat("long-core-", 40), "flow", 9)
		event.AssetTag = tag
		record, err := encodeRecord(event)
		if err != nil {
			t.Fatal(err)
		}
		if binary.BigEndian.Uint16(record[4:6]) != 1 {
			t.Fatal("framing compatibility changed")
		}
		if tag == "" && bytes.Contains(record, []byte("asset_tag")) {
			t.Fatal("legacy unknown material gained a stamp")
		}
		actual, _, err := readRecord(bytes.NewReader(record))
		if err != nil || !reflect.DeepEqual(event, actual) {
			t.Fatal("record source material changed", err)
		}
		// Every compressed codec and compacted writer uses these record functions.
		// A second serialization must retain unknown/nonempty and unbounded CoreID.
		again, err := encodeRecord(actual)
		if err != nil || !bytes.Equal(record, again) {
			t.Fatal("re-encoding lost optional material", err)
		}
	}
	var legacy Envelope
	if err := json.Unmarshal([]byte(`{"node_id":7,"type":"stats.sampled","payload":{}}`), &legacy); err != nil || legacy.AssetTag != "" {
		t.Fatal("old envelope manufactured a current label", err)
	}
}

func TestSemanticCompactionPreservesMixedAssetLineage(t *testing.T) {
	for _, kind := range []string{"flow.updated", "stats.sampled"} {
		for _, middle := range []string{"asset-b", ""} {
			t.Run(kind+"/"+middle, func(t *testing.T) {
				events := []Envelope{semanticTestEvent("a1", kind, "same-core", "same-flow", 1), semanticTestEvent("b2", kind, "same-core", "same-flow", 2), semanticTestEvent("a3", kind, "same-core", "same-flow", 3)}
				events[0].AssetTag = "asset-a"
				events[1].AssetTag = middle
				events[2].AssetTag = "asset-a"
				events[0].Payload = []byte(`{"bytes_up":1}`)
				events[1].Payload = []byte(`{"bytes_up":2}`)
				events[2].Payload = events[0].Payload
				got, flow, stats, _ := compactSemanticEvents(events, DefaultConfig().Compaction)
				if flow != 0 || stats != 0 || !reflect.DeepEqual(got, events) {
					t.Fatal("mixed lineage removed or reordered source facts")
				}
			})
		}
	}
	for _, tag := range []string{"", "single-asset"} {
		events := []Envelope{semanticTestEvent("one", "flow.updated", "same-core", "same-flow", 1), semanticTestEvent("two", "flow.updated", "same-core", "same-flow", 2)}
		for i := range events {
			events[i].AssetTag = tag
		}
		got, saved, _, _ := compactSemanticEvents(events, DefaultConfig().Compaction)
		if len(got) != 1 || saved != 1 || got[0].ID != "two" {
			t.Fatal("single-label/legacy savings behavior changed")
		}
	}
}

func TestCompactedCodecWritersRetainAssetAndLegacyMaterials(t *testing.T) {
	for _, codec := range []string{CompressionNone, CompressionZstd, CompressionLZ4} {
		t.Run(codec, func(t *testing.T) {
			cfg := testFileConfig(t)
			spool := &FileSpool{cfg: cfg}
			events := []Envelope{semanticTestEvent("legacy", "flow.updated", strings.Repeat("core", 80), "flow", 1), semanticTestEvent("known", "stats.sampled", strings.Repeat("core", 80), "flow", 2)}
			events[1].AssetTag = strings.Repeat("a", 64)
			suffix := segmentReadySuffix
			if codec == CompressionZstd {
				suffix = segmentZstdSuffix
			}
			if codec == CompressionLZ4 {
				suffix = segmentLZ4Suffix
			}
			path := segmentPath(cfg.Directory, 1, suffix)
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = spool.writeCompactedSegment(file, codec, events)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal("codec writer", err, closeErr)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := readWholeSegment(context.Background(), segmentFile{sequence: 1, path: path, codec: codec, size: info.Size()})
			if err != nil || !reflect.DeepEqual(got, events) {
				t.Fatal("raw/compressed compacted representation lost original material", err)
			}
			assertAssetSpoolCheckpointLifecycle(t, cfg, events)
		})
	}
}

func TestNodeAssetSpoolAppendCloseAndCheckpointRecovery(t *testing.T) {
	cfg := testFileConfig(t)
	events := []Envelope{testEnvelope("old-unknown", 1), testEnvelope("captured-known", 2)}
	events[1].AssetTag = strings.Repeat("a", 64)
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	for _, event := range events {
		if err := spool.Append(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	assertAssetSpoolCheckpointLifecycle(t, cfg, events)
}

func assertAssetSpoolCheckpointLifecycle(t *testing.T, cfg Config, events []Envelope) {
	t.Helper()
	reopened, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	batch, err := reopened.ReadBatch(context.Background(), len(events)+1)
	if err != nil || !reflect.DeepEqual(batch.Events, events) {
		t.Fatal("real spool startup lost captured/unknown source material", err)
	}
	if err := reopened.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	consumed, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumed.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = consumed.Close() })
	again, err := consumed.ReadBatch(context.Background(), len(events)+1)
	if err != nil || len(again.Events) != 0 {
		t.Fatal("persisted checkpoint replayed consumed identity materials", err)
	}
	if err := consumed.Close(); err != nil {
		t.Fatal(err)
	}
}
