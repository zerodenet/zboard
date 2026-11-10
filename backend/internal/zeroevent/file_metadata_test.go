package zeroevent

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func compressedFixture(t *testing.T, dir string, count int) (string, []byte) {
	t.Helper()
	var raw bytes.Buffer
	for i := 0; i < count; i++ {
		record, err := encodeRecord(testEnvelope(string(rune('a'+i)), uint64(i+1)))
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(record)
	}
	var data bytes.Buffer
	// Tiny target intentionally places each complete record in its own block.
	if err := compressRawSegmentToZstd(bytes.NewReader(raw.Bytes()), &data, 1, 1); err != nil {
		t.Fatal(err)
	}
	path := segmentPath(dir, 1, segmentZstdSuffix)
	return path, append([]byte(nil), data.Bytes()...)
}
func TestMetadataCountsNeverAuthorizeCorruptRecordCommit(t *testing.T) {
	cfg := testFileConfig(t)
	path, data := compressedFixture(t, cfg.Directory, 1)
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	status := spool.Status()
	if status.PendingEvents != 1 || status.PendingUnknown || status.CompressedOriginalBytes == 0 {
		t.Fatal("header counts changed", status)
	}
	if batch, err := spool.ReadBatch(context.Background(), 1); err == nil || len(batch.Events) != 0 {
		t.Fatal("corrupt body reached projection", batch, err)
	}
	if err := spool.Commit(context.Background(), Checkpoint{Segment: 1, Block: 1, Record: 1}); err == nil {
		t.Fatal("metadata count authorized corrupt checkpoint")
	}
	if spool.checkpoint != (Checkpoint{}) {
		t.Fatal("corrupt body advanced checkpoint")
	}
}
func TestTruncatedCompressedMetadataIsUnknownInsteadOfZero(t *testing.T) {
	cfg := testFileConfig(t)
	path, data := compressedFixture(t, cfg.Directory, 1)
	if err := os.WriteFile(path, data[:len(data)-1], 0600); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	status := spool.Status()
	if !status.PendingUnknown || status.ReadError == "" {
		t.Fatal("damaged metadata looked like an empty healthy queue", status)
	}
}
func TestCommitVerifiedCompressedPrefixDoesNotRequireReadingCorruptFutureBlock(t *testing.T) {
	cfg := testFileConfig(t)
	path, data := compressedFixture(t, cfg.Directory, 2)
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	first, err := spool.ReadBatch(context.Background(), 1)
	if err != nil || len(first.Events) != 1 || first.Events[0].ID != "a" {
		t.Fatal(first, err)
	}
	if err := spool.Commit(context.Background(), first.Next); err != nil {
		t.Fatal("verified prefix blocked by unread future body", err)
	}
	if next, err := spool.ReadBatch(context.Background(), 1); err == nil || len(next.Events) != 0 {
		t.Fatal("corrupt future block was accepted", next, err)
	}
	if spool.checkpoint != first.Next {
		t.Fatal("failed future block advanced checkpoint")
	}
}

func TestDamagedBlockRecordCountCannotHideUnreadEventsAtBatchBoundary(t *testing.T) {
	cfg := testFileConfig(t)
	var raw bytes.Buffer
	for _, id := range []string{"first", "second"} {
		record, err := encodeRecord(testEnvelope(id, 1))
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(record)
	}
	var compressed bytes.Buffer
	if err := compressRawSegmentToZstd(bytes.NewReader(raw.Bytes()), &compressed, 1<<20, 1); err != nil {
		t.Fatal(err)
	}
	data := compressed.Bytes()
	// ZEB1's count is in the framing header, outside the decoded payload CRC.
	// Corrupt it from 2 to 1 while leaving two valid records in that payload.
	data[16], data[17], data[18], data[19] = 0, 0, 0, 1
	if err := os.WriteFile(segmentPath(cfg.Directory, 1, segmentZstdSuffix), data, 0600); err != nil {
		t.Fatal(err)
	}
	spool, _ := NewFileSpool(cfg)
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if batch, err := spool.ReadBatch(context.Background(), 1); err == nil || len(batch.Events) != 0 {
		t.Fatal("corrupt count exposed an apparently complete prefix", batch, err)
	}
	if spool.checkpoint != (Checkpoint{}) {
		t.Fatal("corrupt framing advanced checkpoint")
	}
}
