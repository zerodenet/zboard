package zeroevent

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func rawIndexFixture(t *testing.T, count int) (*FileSpool, segmentFile, []int64) {
	t.Helper()
	cfg := testFileConfig(t)
	path := segmentPath(cfg.Directory, 1, segmentActiveSuffix)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	ends := []int64{0}
	for i := range count {
		record, err := encodeRecord(testEnvelope(fmt.Sprintf("event-%d", i), uint64(i+1)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(record); err != nil {
			t.Fatal(err)
		}
		ends = append(ends, ends[len(ends)-1]+int64(len(record)))
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return &FileSpool{cfg: cfg}, segmentFile{sequence: 1, path: path, active: true, codec: CompressionNone, size: ends[count]}, ends
}

func TestRawIndexResumesWithinSparsePointsAndOlderDurableSnapshot(t *testing.T) {
	spool, segment, ends := rawIndexFixture(t, 137)
	ctx := context.Background()
	if _, err := spool.rawPosition(ctx, segment, 0); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{137, 96, 79, 1, 0} {
		snapshot := segment
		snapshot.size = ends[count]
		for start := 0; start <= count; start++ {
			events, next, err := spool.readSegmentBatch(ctx, snapshot, Checkpoint{Segment: 1, Record: uint64(start)}, 17)
			if err != nil {
				t.Fatal(err)
			}
			want := min(17, count-start)
			if len(events) != want || next.Record != uint64(start+want) {
				t.Fatalf("count=%d start=%d length=%d next=%+v", count, start, len(events), next)
			}
			for i, event := range events {
				if event.Sequence != uint64(start+i+1) {
					t.Fatalf("unexpected event %+v", event)
				}
			}
			position, err := spool.rawPosition(ctx, snapshot, uint64(start))
			if err != nil || position.total != uint64(count) {
				t.Fatalf("position=%+v err=%v", position, err)
			}
		}
	}
	if spool.rawIndexes.entries[1].records != 137 {
		t.Fatal("older snapshot discarded verified suffix")
	}
}

func TestRawIndexTracksPartialAppendAndRejectsInvalidCheckpoint(t *testing.T) {
	spool, segment, _ := rawIndexFixture(t, 33)
	ctx := context.Background()
	if _, err := spool.rawPosition(ctx, segment, 0); err != nil {
		t.Fatal(err)
	}
	record, err := encodeRecord(testEnvelope("appended", 34))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(segment.path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	half := len(record) / 2
	for i, part := range [][]byte{record[:half], record[half:]} {
		if _, err := file.Write(part); err != nil {
			t.Fatal(err)
		}
		segment.size += int64(len(part))
		position, err := spool.rawPosition(ctx, segment, 0)
		if err != nil || position.total != uint64(33+i) {
			t.Fatalf("part=%d position=%+v err=%v", i, position, err)
		}
	}
	if err := spool.validateCheckpoint(Checkpoint{Segment: 1, Record: 35}); err == nil {
		t.Fatal("accepted checkpoint past end")
	}
	if err := spool.validateCheckpoint(Checkpoint{Segment: 1, Block: 1}); err == nil {
		t.Fatal("accepted compressed checkpoint for raw segment")
	}
	if err := spool.validateCheckpoint(Checkpoint{Segment: 1, Record: 34}); err != nil {
		t.Fatal(err)
	}
}

func TestRawIndexInvalidatesReplacementTruncationAndCorruption(t *testing.T) {
	for _, operation := range []string{"replace", "truncate", "corrupt"} {
		t.Run(operation, func(t *testing.T) {
			spool, segment, ends := rawIndexFixture(t, 40)
			ctx := context.Background()
			if _, err := spool.rawPosition(ctx, segment, 0); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "replace":
				record, _ := encodeRecord(testEnvelope("replacement", 100))
				if err := os.WriteFile(segment.path+".new", record, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(segment.path+".new", segment.path); err != nil {
					t.Fatal(err)
				}
				segment.size = int64(len(record))
			case "truncate":
				segment.size = ends[1]
				if err := os.Truncate(segment.path, segment.size); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				file, err := os.OpenFile(segment.path, os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteAt([]byte{0}, 0); err != nil {
					t.Fatal(err)
				}
				file.Close()
				later := time.Now().Add(time.Hour)
				if err := os.Chtimes(segment.path, later, later); err != nil {
					t.Fatal(err)
				}
			}
			position, err := spool.rawPosition(ctx, segment, 0)
			if operation == "corrupt" {
				if err == nil {
					t.Fatal("cached offsets hid corrupted header")
				}
				if _, err := spool.rawPosition(ctx, segment, 0); err == nil {
					t.Fatal("retry hid corrupted header")
				}
			} else if err != nil || position.total != 1 {
				t.Fatalf("position=%+v err=%v", position, err)
			}
		})
	}
}

func TestRawIndexBoundsPointsAndSegments(t *testing.T) {
	index := rawSegmentIndex{stride: 32, offsets: []int64{0}}
	for record := 1; record <= rawIndexPoints*128; record++ {
		index.appendBoundary(7)
		if len(index.offsets) > rawIndexPoints {
			t.Fatal("unbounded point cache")
		}
		if record%8192 == 0 {
			for i, offset := range index.offsets {
				if offset != int64(uint64(i)*index.stride)*7 {
					t.Fatalf("point=%d offset=%d stride=%d", i, offset, index.stride)
				}
			}
		}
	}
	spool, segment, _ := rawIndexFixture(t, 1)
	for seq := uint64(1); seq <= rawIndexSegments+3; seq++ {
		segment.sequence = seq
		if _, err := spool.rawPosition(context.Background(), segment, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(spool.rawIndexes.entries) != rawIndexSegments || spool.rawIndexes.entries[1] != nil {
		t.Fatal("segment cache failed to evict oldest entry")
	}
}
