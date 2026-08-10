package zeroevent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rawRecords(t *testing.T, count int) []byte {
	t.Helper()
	var raw bytes.Buffer
	for index := 1; index <= count; index++ {
		record, err := encodeRecord(testEnvelope("compressed", uint64(index)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	return raw.Bytes()
}

func waitForCompressedReplacement(t *testing.T, directory string, sequence uint64) string {
	t.Helper()
	compressedPath := segmentPath(directory, sequence, segmentZstdSuffix)
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
			t.Fatalf("segment %d was not fully replaced by compressed representation", sequence)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestZstdCompressedSegmentReadsLegacyAndBlockCheckpoints(t *testing.T) {
	raw := rawRecords(t, 5)
	record, err := encodeRecord(testEnvelope("sample", 1))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	if err := compressRawSegmentToZstd(bytes.NewReader(raw), &compressed, int64(len(record)*2+1), 1); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "00000000000000000001"+segmentZstdSuffix)
	if err := os.WriteFile(path, compressed.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	if records, _, err := inspectCompressedSegment(path); err != nil || records != 5 {
		t.Fatalf("inspectCompressedSegment() records=%d err=%v", records, err)
	}
	segment := segmentFile{sequence: 1, path: path, codec: CompressionZstd, size: int64(compressed.Len())}

	legacy := Checkpoint{Segment: 1, Record: 2}
	events, next, err := readCompressedSegmentBatch(context.Background(), segment, legacy, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence != 3 || events[1].Sequence != 4 {
		t.Fatalf("legacy resumed events = %+v", events)
	}
	if next.Block == 0 {
		t.Fatalf("compressed reader did not upgrade checkpoint: %+v", next)
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

func TestFileSpoolCompressesOnlySealedSegments(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compression.Enabled = true
	cfg.Compression.Algorithm = CompressionZstd
	cfg.Compression.Level = 1
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

	waitForCompressedReplacement(t, cfg.Directory, 1)
	if _, err := os.Stat(segmentPath(cfg.Directory, 2, segmentActiveSuffix)); err != nil {
		t.Fatalf("active segment must remain uncompressed: %v", err)
	}

	batch, err := spool.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].Sequence != 1 || batch.Events[1].Sequence != 2 {
		t.Fatalf("unexpected mixed compressed/active batch: %+v", batch.Events)
	}
}

func TestFileSpoolRecoversCompressedSegmentWithLegacyCheckpoint(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compression.Enabled = true
	cfg.Compression.Algorithm = CompressionZstd
	cfg.Compression.Level = 1
	cfg.Compression.BlockSize = 256

	rawPath := segmentPath(cfg.Directory, 1, segmentReadySuffix)
	if err := os.WriteFile(rawPath, rawRecords(t, 4), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeCheckpoint(cfg.Directory, Checkpoint{Segment: 1, Record: 2}); err != nil {
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
	compressedPath := waitForCompressedReplacement(t, cfg.Directory, 1)

	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].Sequence != 3 || batch.Events[1].Sequence != 4 {
		t.Fatalf("recovered events = %+v", batch.Events)
	}
	if batch.Next.Block == 0 {
		t.Fatalf("recovered compressed checkpoint stayed legacy: %+v", batch.Next)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(compressedPath); !os.IsNotExist(err) {
		t.Fatalf("fully consumed compressed segment still exists: %v", err)
	}
}

func TestListSegmentsPrefersRawCrashFallbackOverCompressedDuplicate(t *testing.T) {
	directory := t.TempDir()
	rawPath := segmentPath(directory, 7, segmentReadySuffix)
	if err := os.WriteFile(rawPath, rawRecords(t, 1), 0o640); err != nil {
		t.Fatal(err)
	}
	compressedPath := segmentPath(directory, 7, segmentZstdSuffix)
	if err := os.WriteFile(compressedPath, []byte("incomplete"), 0o640); err != nil {
		t.Fatal(err)
	}
	segments, err := listSegments(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].path != rawPath || segments[0].codec != CompressionNone {
		t.Fatalf("duplicate recovery chose unsafe representation: %+v", segments)
	}
}
