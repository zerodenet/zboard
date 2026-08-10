package zeroevent

import (
	"context"
	"testing"
)

func TestFileSpoolStatusReportsSemanticCompactionTotals(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	writeRawTestSegment(t, cfg.Directory, 1, []Envelope{
		semanticTestEvent("flow-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("flow-2", "flow.updated", "core-a", "flow-a", 2),
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
	if result.FlowUpdatesSaved != 1 {
		t.Fatalf("unexpected compaction result: %+v", result)
	}
	status := spool.Status()
	if status.CompactionRuns != 1 || status.CompactedSegments != 1 || status.CompactedEvents != 1 || status.CompactedFlowUpdates != 1 {
		t.Fatalf("unexpected compaction status: %+v", status)
	}
	if status.CompactedNodeStats != 0 || status.CompactionUnsafeSkipped != 0 {
		t.Fatalf("unexpected compaction detail counters: %+v", status)
	}
	if status.CompactionBytesSaved <= 0 {
		t.Fatalf("expected positive raw compaction byte savings: %+v", status)
	}
}

func TestFileSpoolCompactsHistoricalZstdWhenCompressionNowDisabled(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	cfg.Compression.Enabled = false
	cfg.Compression.Algorithm = CompressionNone
	cfg.Compression.BlockSize = 0
	cfg.Compression.Workers = 0
	cfg.Compression.Level = 0
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
	if result.FlowUpdatesSaved != 2 {
		t.Fatalf("unexpected historical zstd compaction: %+v", result)
	}
	if records, _, err := inspectCompressedSegment(path); err != nil || records != 1 {
		t.Fatalf("historical zstd representation was not compacted safely: records=%d err=%v", records, err)
	}
}
