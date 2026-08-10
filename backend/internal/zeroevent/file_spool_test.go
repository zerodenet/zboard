package zeroevent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testFileConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.Flush.Interval = time.Millisecond
	cfg.Flush.MaxBatch = 64
	cfg.Segment.MaxAge = time.Hour
	cfg.Compression.Enabled = false
	cfg.Compression.Algorithm = CompressionNone
	cfg.Retention.Consumed = 0
	return cfg
}

func testEnvelope(id string, sequence uint64) Envelope {
	return Envelope{
		ID:         id,
		NodeID:     4,
		Type:       "flow.updated",
		OccurredAt: time.Unix(1_786_000_000, 0).UTC(),
		FlowID:     "flow-1",
		Sequence:   sequence,
		Payload:    json.RawMessage(`{"bytes_up":73,"bytes_down":146}`),
	}
}

func TestFileSpoolAppendReadCommitAndRecover(t *testing.T) {
	cfg := testFileConfig(t)
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 3; i++ {
		if err := spool.Append(context.Background(), testEnvelope(string(rune('a'+i-1)), i)); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := spool.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].Sequence != 1 || batch.Events[1].Sequence != 2 {
		t.Fatalf("unexpected first batch: %+v", batch.Events)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	batch, err = reopened.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Events[0].Sequence != 3 {
		t.Fatalf("unexpected recovered batch: %+v", batch.Events)
	}
}

func TestFileSpoolConcurrentAppendUsesDurableBatches(t *testing.T) {
	cfg := testFileConfig(t)
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	const count = 40
	var wait sync.WaitGroup
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errors <- spool.Append(context.Background(), testEnvelope("event", uint64(index+1)))
		}(i)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	batch, err := spool.ReadBatch(context.Background(), count+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != count {
		t.Fatalf("ReadBatch() count = %d, want %d", len(batch.Events), count)
	}
	if status := spool.Status(); status.PendingEvents != count {
		t.Fatalf("Status().PendingEvents = %d, want %d", status.PendingEvents, count)
	}
}

func TestRecoverSegmentsTruncatesInterruptedFinalRecord(t *testing.T) {
	cfg := testFileConfig(t)
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := spool.Append(context.Background(), testEnvelope("complete", 1)); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	segments, err := listSegments(cfg.Directory)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments = %+v, err = %v", segments, err)
	}
	before := segments[0].size
	file, err := os.OpenFile(segments[0].path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0x5a, 0x45, 0x56}); err != nil {
		t.Fatal(err)
	}
	file.Close()

	reopened, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	info, err := os.Stat(segments[0].path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != before {
		t.Fatalf("recovered size = %d, want %d", info.Size(), before)
	}
	batch, err := reopened.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("batch = %+v, err = %v", batch, err)
	}
}

func TestFileSpoolRotatesBySizeAndCleansCommittedReadySegment(t *testing.T) {
	cfg := testFileConfig(t)
	record, err := encodeRecord(testEnvelope("sample", 1))
	if err != nil {
		t.Fatal(err)
	}
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
	segments, err := listSegments(cfg.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].active || !segments[1].active {
		t.Fatalf("unexpected rotated segments: %+v", segments)
	}
	batch, err := spool.ReadBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(segmentPath(cfg.Directory, segments[0].sequence, segmentReadySuffix)); !os.IsNotExist(err) {
		t.Fatalf("committed ready segment still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Directory, checkpointFileName)); err != nil {
		t.Fatalf("checkpoint missing: %v", err)
	}
}

func TestFileSpoolRejectsCheckpointBeyondDurableRecords(t *testing.T) {
	cfg := testFileConfig(t)
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if err := spool.Append(context.Background(), testEnvelope("one", 1)); err != nil {
		t.Fatal(err)
	}
	batch, err := spool.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	batch.Next.Record++
	if err := spool.Commit(context.Background(), batch.Next); err == nil {
		t.Fatal("Commit() error = nil")
	}
}

func TestFileSpoolSplitsOneFlushBatchAcrossSegments(t *testing.T) {
	cfg := testFileConfig(t)
	record, err := encodeRecord(testEnvelope("sample", 1))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Segment.MaxSize = int64(len(record) + 8)
	cfg.Flush.Interval = 20 * time.Millisecond
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	const count = 8
	start := make(chan struct{})
	var wait sync.WaitGroup
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			errors <- spool.Append(context.Background(), testEnvelope("batched", uint64(index+1)))
		}(i)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	segments, err := listSegments(cfg.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != count {
		t.Fatalf("segment count = %d, want %d", len(segments), count)
	}
	for _, segment := range segments {
		if segment.size > cfg.Segment.MaxSize {
			t.Fatalf("segment %d size = %d, max = %d", segment.sequence, segment.size, cfg.Segment.MaxSize)
		}
	}
}

func TestRecoverSegmentsQuarantinesCorruptRecord(t *testing.T) {
	cfg := testFileConfig(t)
	path := segmentPath(cfg.Directory, 1, segmentReadySuffix)
	if err := os.WriteFile(path, []byte("not-a-record"), 0o640); err != nil {
		t.Fatal(err)
	}
	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil")
	}
	matches, err := filepath.Glob(path + ".corrupt.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantined files = %v", matches)
	}
}
