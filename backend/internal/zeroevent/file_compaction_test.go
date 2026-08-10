package zeroevent

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func semanticTestEvent(id, eventType, instanceID, flowID string, sequence uint64) Envelope {
	return Envelope{
		ID:             id,
		NodeID:         7,
		Type:           eventType,
		CoreInstanceID: instanceID,
		FlowID:         flowID,
		Sequence:       sequence,
		Payload:        []byte(`{}`),
	}
}

func writeRawTestSegment(t *testing.T, directory string, sequence uint64, events []Envelope) string {
	t.Helper()
	var raw bytes.Buffer
	for _, event := range events {
		record, err := encodeRecord(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	path := segmentPath(directory, sequence, segmentReadySuffix)
	if err := os.WriteFile(path, raw.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCompressedTestSegment(t *testing.T, directory string, sequence uint64, events []Envelope) string {
	t.Helper()
	var raw bytes.Buffer
	for _, event := range events {
		record, err := encodeRecord(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	var compressed bytes.Buffer
	if err := compressRawSegmentToZstd(bytes.NewReader(raw.Bytes()), &compressed, 256, 1); err != nil {
		t.Fatal(err)
	}
	path := segmentPath(directory, sequence, segmentZstdSuffix)
	if err := os.WriteFile(path, compressed.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompactSemanticEventsKeepsLatestSafeSnapshotsInOriginalOrder(t *testing.T) {
	cfg := DefaultConfig().Compaction
	events := []Envelope{
		semanticTestEvent("flow-a-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("stats-1", "stats.sampled", "core-a", "", 2),
		semanticTestEvent("critical", "flow.completed", "core-a", "flow-a", 3),
		semanticTestEvent("flow-b", "flow.updated", "core-a", "flow-b", 4),
		semanticTestEvent("flow-a-2", "flow.updated", "core-a", "flow-a", 5),
		semanticTestEvent("stats-2", "stats.sampled", "core-a", "", 6),
		semanticTestEvent("flow-a-next-core", "flow.updated", "core-b", "flow-a", 1),
		semanticTestEvent("unsafe-old-record", "flow.updated", "", "flow-a", 7),
	}

	survivors, flowSaved, statsSaved, unsafe := compactSemanticEvents(events, cfg)
	if flowSaved != 1 || statsSaved != 1 || unsafe != 1 {
		t.Fatalf("saved flow=%d stats=%d unsafe=%d", flowSaved, statsSaved, unsafe)
	}
	want := []string{"critical", "flow-b", "flow-a-2", "stats-2", "flow-a-next-core", "unsafe-old-record"}
	if len(survivors) != len(want) {
		t.Fatalf("survivors=%v", survivors)
	}
	for index, id := range want {
		if survivors[index].ID != id {
			t.Fatalf("survivor[%d]=%q, want %q", index, survivors[index].ID, id)
		}
	}
}

func TestCompactSemanticEventsKeepsConflictingDuplicateSequence(t *testing.T) {
	cfg := DefaultConfig().Compaction
	events := []Envelope{
		semanticTestEvent("first", "flow.updated", "core-a", "flow-a", 9),
		semanticTestEvent("duplicate", "flow.updated", "core-a", "flow-a", 9),
		semanticTestEvent("older", "flow.updated", "core-a", "flow-a", 8),
	}
	survivors, flowSaved, _, _ := compactSemanticEvents(events, cfg)
	if flowSaved != 0 || len(survivors) != 3 {
		t.Fatalf("conflicting sequence was compacted: saved=%d survivors=%v", flowSaved, survivors)
	}
}

func TestFileSpoolCompactsRawSealedSegment(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	cfg.Compaction.MergeFlowUpdates = true
	cfg.Compaction.MergeNodeStats = true
	path := writeRawTestSegment(t, cfg.Directory, 1, []Envelope{
		semanticTestEvent("flow-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("stats-1", "stats.sampled", "core-a", "", 2),
		semanticTestEvent("flow-2", "flow.updated", "core-a", "flow-a", 3),
		semanticTestEvent("stats-2", "stats.sampled", "core-a", "", 4),
	})
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	result, err := spool.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Segments != 1 || result.FlowUpdatesSaved != 1 || result.NodeStatsSaved != 1 {
		t.Fatalf("unexpected compaction result: %+v", result)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("raw compaction did not shrink segment: before=%d after=%d", before.Size(), after.Size())
	}

	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].ID != "flow-2" || batch.Events[1].ID != "stats-2" {
		t.Fatalf("unexpected compacted raw events: %+v", batch.Events)
	}
}

func TestFileSpoolCompactsCompressedSegmentWithoutExpandingToRaw(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	path := writeCompressedTestSegment(t, cfg.Directory, 1, []Envelope{
		semanticTestEvent("flow-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("flow-2", "flow.updated", "core-a", "flow-a", 2),
		semanticTestEvent("flow-3", "flow.updated", "core-a", "flow-a", 3),
	})

	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	result, err := spool.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Segments != 1 || result.FlowUpdatesSaved != 2 {
		t.Fatalf("unexpected compressed compaction: %+v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("compressed representation disappeared: %v", err)
	}
	if _, err := os.Stat(segmentPath(cfg.Directory, 1, segmentReadySuffix)); !os.IsNotExist(err) {
		t.Fatalf("compressed compaction published an intermediate raw segment: %v", err)
	}
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Events[0].ID != "flow-3" {
		t.Fatalf("unexpected compacted compressed events: %+v", batch.Events)
	}
}

func TestFileSpoolCompactionSkipsCheckpointAndInflightHorizon(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	firstPath := writeRawTestSegment(t, cfg.Directory, 1, []Envelope{
		semanticTestEvent("first-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("first-2", "flow.updated", "core-a", "flow-a", 2),
	})
	secondPath := writeRawTestSegment(t, cfg.Directory, 2, []Envelope{
		semanticTestEvent("second-1", "flow.updated", "core-a", "flow-b", 3),
		semanticTestEvent("second-2", "flow.updated", "core-a", "flow-b", 4),
	})

	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	batch, err := spool.ReadBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Next.Segment != 1 {
		t.Fatalf("unexpected in-flight batch: %+v", batch)
	}
	result, err := spool.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Segments != 1 || result.FlowUpdatesSaved != 1 {
		t.Fatalf("future segment was not compacted: %+v", result)
	}
	if records, _, err := inspectRawSegment(firstPath, fileSize(t, firstPath), false); err != nil || records != 2 {
		t.Fatalf("in-flight segment changed: records=%d err=%v", records, err)
	}
	if records, _, err := inspectRawSegment(secondPath, fileSize(t, secondPath), false); err != nil || records != 1 {
		t.Fatalf("future segment not compacted: records=%d err=%v", records, err)
	}

	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	result, err = spool.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Segments != 0 {
		t.Fatalf("checkpoint segment must remain stable: %+v", result)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
