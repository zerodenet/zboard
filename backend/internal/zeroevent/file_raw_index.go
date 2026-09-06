package zeroevent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

const rawIndexSegments = 64
const rawIndexPoints = 8192

// Only verified record boundaries are cached. The durable checkpoint remains a
// record number; losing this bounded process-local index never loses an event.
// Callers hold segmentMu across lookup and use, so replacement cannot race use.
type rawSegmentIndexes struct {
	mu      sync.Mutex
	clock   uint64
	entries map[uint64]*rawSegmentIndex
}

type rawSegmentIndex struct {
	info       os.FileInfo
	used       uint64
	stride     uint64
	offsets    []int64
	records    uint64
	validBytes int64
}

type rawSegmentPosition struct {
	record     uint64
	offset     int64
	total      uint64
	validBytes int64
}

func (index *rawSegmentIndex) appendBoundary(size int64) {
	index.validBytes += size
	index.records++
	if index.records%index.stride != 0 {
		return
	}
	if len(index.offsets) == rawIndexPoints {
		for i := 0; i < len(index.offsets)/2; i++ {
			index.offsets[i] = index.offsets[2*i]
		}
		index.offsets = index.offsets[:len(index.offsets)/2]
		index.stride *= 2
	}
	if index.records%index.stride == 0 {
		index.offsets = append(index.offsets, index.validBytes)
	}
}

func (s *FileSpool) rawPosition(ctx context.Context, segment segmentFile, record uint64) (rawSegmentPosition, error) {
	s.rawIndexes.mu.Lock()
	defer s.rawIndexes.mu.Unlock()
	file, err := os.Open(segment.path)
	if err != nil {
		return rawSegmentPosition{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return rawSegmentPosition{}, err
	}
	cache := &s.rawIndexes
	if cache.entries == nil {
		cache.entries = make(map[uint64]*rawSegmentIndex)
	}
	index := cache.entries[segment.sequence]
	// Active files only grow. Replacement, truncation, or a modification that
	// is not an append invalidates previously verified offsets and counts.
	if index != nil && (!os.SameFile(index.info, info) || info.Size() < index.info.Size() ||
		(!info.ModTime().Equal(index.info.ModTime()) && (!segment.active || info.Size() == index.info.Size()))) {
		delete(cache.entries, segment.sequence)
		index = nil
	}
	if index == nil {
		if len(cache.entries) >= rawIndexSegments {
			var oldest uint64
			var selected bool
			for sequence, entry := range cache.entries {
				if !selected || entry.used < cache.entries[oldest].used {
					oldest, selected = sequence, true
				}
			}
			delete(cache.entries, oldest)
		}
		index = &rawSegmentIndex{info: info, stride: 32, offsets: []int64{0}}
		cache.entries[segment.sequence] = index
	}
	cache.clock++
	index.used = cache.clock
	index.info = info
	if segment.size < index.validBytes {
		// Status may have observed a later append than ReadBatch's durable
		// snapshot. Count only the older prefix, without discarding the index.
		point := sort.Search(len(index.offsets), func(i int) bool { return index.offsets[i] > segment.size }) - 1
		prefix := rawSegmentIndex{records: uint64(point) * index.stride, validBytes: index.offsets[point]}
		if err := scanRawIndex(ctx, file, segment, &prefix, false); err != nil {
			return rawSegmentPosition{}, err
		}
		return index.position(record, prefix.records, prefix.validBytes), nil
	}
	if err := scanRawIndex(ctx, file, segment, index, true); err != nil {
		// A failed scan must not hide corruption on a subsequent call.
		delete(cache.entries, segment.sequence)
		return rawSegmentPosition{}, err
	}
	return index.position(record, index.records, index.validBytes), nil
}

func (index *rawSegmentIndex) position(record, total uint64, validBytes int64) rawSegmentPosition {
	if record > total {
		record = total
	}
	point := record / index.stride
	return rawSegmentPosition{record: point * index.stride, offset: index.offsets[point], total: total, validBytes: validBytes}
}

func scanRawIndex(ctx context.Context, file *os.File, segment segmentFile, index *rawSegmentIndex, retain bool) error {
	if _, err := file.Seek(index.validBytes, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(io.LimitReader(file, segment.size-index.validBytes))
	for index.validBytes < segment.size {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, size, err := readRecord(reader)
		if err != nil {
			if segment.active && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return nil
			}
			return err
		}
		if retain {
			index.appendBoundary(size)
		} else {
			index.records++
			index.validBytes += size
		}
	}
	return nil
}

func (s *FileSpool) readSegmentBatch(ctx context.Context, segment segmentFile, checkpoint Checkpoint, limit int) ([]Envelope, Checkpoint, error) {
	if segment.codec != CompressionNone || checkpoint.Block != 0 {
		return readSegmentBatch(ctx, segment, checkpoint, limit)
	}
	position, err := s.rawPosition(ctx, segment, checkpoint.Record)
	if err != nil {
		return nil, checkpoint, err
	}
	events, next, err := readRawSegmentBatchAt(ctx, segment, checkpoint.Record, limit, position.record, position.offset)
	return events, Checkpoint{Segment: segment.sequence, Record: next}, err
}

func (s *FileSpool) inspectSegment(segment segmentFile) (uint64, int64, error) {
	if segment.codec != CompressionNone {
		return inspectSegment(segment.path, segment.size, segment.active)
	}
	position, err := s.rawPosition(context.Background(), segment, 0)
	return position.total, position.validBytes, err
}
