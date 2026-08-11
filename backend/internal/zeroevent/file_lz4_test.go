package zeroevent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForCodecReplacement(t *testing.T, directory string, sequence uint64, suffix string) string {
	t.Helper()
	compressedPath := segmentPath(directory, sequence, suffix)
	rawPath := segmentPath(directory, sequence, segmentReadySuffix)
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, compressedErr := os.Stat(compressedPath)
		_, rawErr := os.Stat(rawPath)
		if compressedErr == nil && os.IsNotExist(rawErr) {
			return compressedPath
		}
		if compressedErr != nil && !os.IsNotExist(compressedErr) {
			t.Fatalf("stat compressed segment: %v", compressedErr)
		}
		if rawErr != nil && !os.IsNotExist(rawErr) {
			t.Fatalf("stat raw segment: %v", rawErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("segment %d was not fully replaced by %s", sequence, suffix)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLZ4CompressedSegmentReadsLegacyAndBlockCheckpoints(t *testing.T) {
	raw := rawRecords(t, 5)
	record, err := encodeRecord(testEnvelope("sample", 1))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	if err := compressRawSegmentWithCodec(bytes.NewReader(raw), &compressed, int64(len(record)*2+1), CompressionLZ4, 1, 2); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "00000000000000000001"+segmentLZ4Suffix)
	if err := os.WriteFile(path, compressed.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	if records, _, err := inspectCompressedSegment(path); err != nil || records != 5 {
		t.Fatalf("inspectCompressedSegment() records=%d err=%v", records, err)
	}
	segment := segmentFile{sequence: 1, path: path, codec: CompressionLZ4, size: int64(compressed.Len())}

	events, next, err := readCompressedSegmentBatch(context.Background(), segment, Checkpoint{Segment: 1, Record: 2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence != 3 || events[1].Sequence != 4 {
		t.Fatalf("legacy resumed events = %+v", events)
	}
	if next.Block == 0 {
		t.Fatalf("lz4 reader did not upgrade checkpoint: %+v", next)
	}
	events, final, err := readCompressedSegmentBatch(context.Background(), segment, next, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 5 {
		t.Fatalf("block resumed events = %+v", events)
	}
	consumed, total, err := compressedCheckpointRecordOffset(path, final)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != 5 || total != 5 {
		t.Fatalf("final checkpoint offset = %d/%d, want 5/5", consumed, total)
	}
}

func TestFileSpoolUsesConfiguredLZ4ForSealedSegments(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compression.Enabled = true
	cfg.Compression.Algorithm = CompressionLZ4
	cfg.Compression.Level = 1
	cfg.Compression.Workers = 2
	record, err := encodeRecord(testEnvelope("sample", 1))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Compression.BlockSize = int64(len(record) * 2)
	cfg.Segment.MaxSize = int64(len(record) + 8)

	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if err := spool.Append(context.Background(), testEnvelope("first", 1)); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(context.Background(), testEnvelope("second", 2)); err != nil {
		t.Fatal(err)
	}

	waitForCodecReplacement(t, cfg.Directory, 1, segmentLZ4Suffix)
	batch, err := spool.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].Sequence != 1 || batch.Events[1].Sequence != 2 {
		t.Fatalf("unexpected lz4/active batch: %+v", batch.Events)
	}
	status := spool.Status()
	if status.CompressionAlgorithm != CompressionLZ4 || status.CompressedSegments < 1 {
		t.Fatalf("unexpected compression status: %+v", status)
	}
	if status.CompressedOriginalBytes <= 0 || status.CompressedStoredBytes <= 0 || status.CompressionRatio <= 0 {
		t.Fatalf("compression metrics were not populated: %+v", status)
	}
}

func TestFileSpoolCompactsLZ4SegmentInPlace(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compression.Algorithm = CompressionLZ4
	cfg.Compression.Level = 1
	cfg.Compaction.Enabled = true
	events := []Envelope{
		semanticTestEvent("flow-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("flow-2", "flow.updated", "core-a", "flow-a", 2),
		semanticTestEvent("flow-3", "flow.updated", "core-a", "flow-a", 3),
	}
	var raw bytes.Buffer
	for _, event := range events {
		record, err := encodeRecord(event)
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(record)
	}
	var compressed bytes.Buffer
	if err := compressRawSegmentWithCodec(bytes.NewReader(raw.Bytes()), &compressed, 256, CompressionLZ4, 1, 1); err != nil {
		t.Fatal(err)
	}
	path := segmentPath(cfg.Directory, 1, segmentLZ4Suffix)
	if err := os.WriteFile(path, compressed.Bytes(), 0o640); err != nil {
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
	if result.Segments != 1 || result.FlowUpdatesSaved != 2 {
		t.Fatalf("unexpected lz4 compaction: %+v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lz4 representation disappeared: %v", err)
	}
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Events[0].ID != "flow-3" {
		t.Fatalf("unexpected compacted lz4 events: %+v", batch.Events)
	}
}
