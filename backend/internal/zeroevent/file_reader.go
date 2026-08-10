package zeroevent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (s *FileSpool) ReadBatch(ctx context.Context, limit int) (Batch, error) {
	if limit <= 0 {
		return Batch{}, errors.New("read batch limit must be greater than zero")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.readMu.Lock()
	defer s.readMu.Unlock()

	// Ask the writer for its durable active offset before taking segmentMu. The
	// writer may need segmentMu to seal a segment, so this ordering prevents a
	// reader/writer lock inversion.
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return Batch{}, err
	}
	s.segmentMu.RLock()
	defer s.segmentMu.RUnlock()

	segments, err := listSegments(s.cfg.Directory)
	if err != nil {
		return Batch{}, err
	}
	if snapshot.sequence != 0 {
		found := false
		for i := range segments {
			if segments[i].sequence == snapshot.sequence {
				segments[i].size = snapshot.size
				found = true
				break
			}
		}
		if !found {
			segments = append(segments, segmentFile{sequence: snapshot.sequence, path: snapshot.path, active: true, codec: CompressionNone, size: snapshot.size})
		}
	}

	s.mu.RLock()
	checkpoint := s.checkpoint
	s.mu.RUnlock()
	batch := Batch{Next: checkpoint}
	for _, segment := range segments {
		if len(batch.Events) >= limit {
			break
		}
		if checkpoint.Segment != 0 && segment.sequence < checkpoint.Segment {
			continue
		}
		start := Checkpoint{Segment: segment.sequence}
		if segment.sequence == checkpoint.Segment {
			start = checkpoint
		}
		events, next, err := readSegmentBatch(ctx, segment, start, limit-len(batch.Events))
		if err != nil {
			return Batch{}, err
		}
		if len(events) == 0 {
			continue
		}
		batch.Events = append(batch.Events, events...)
		batch.Next = next
	}
	return batch, nil
}

func readSegmentBatch(ctx context.Context, segment segmentFile, checkpoint Checkpoint, limit int) ([]Envelope, Checkpoint, error) {
	if segment.codec == CompressionZstd {
		return readCompressedSegmentBatch(ctx, segment, checkpoint, limit)
	}
	if checkpoint.Block != 0 {
		return nil, checkpoint, fmt.Errorf("raw segment %d cannot resume from compressed block %d", segment.sequence, checkpoint.Block)
	}
	events, nextRecord, err := readRawSegmentBatch(ctx, segment, checkpoint.Record, limit)
	if err != nil {
		return nil, checkpoint, err
	}
	return events, Checkpoint{Segment: segment.sequence, Record: nextRecord}, nil
}

func readRawSegmentBatch(ctx context.Context, segment segmentFile, startRecord uint64, limit int) ([]Envelope, uint64, error) {
	file, err := os.Open(segment.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, startRecord, nil
		}
		return nil, startRecord, fmt.Errorf("open event segment for read: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, segment.size))
	var index uint64
	for index < startRecord {
		if err := ctx.Err(); err != nil {
			return nil, index, err
		}
		_, _, err := readRecord(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, index, nil
			}
			return nil, index, fmt.Errorf("skip event record in %s: %w", filepath.Base(segment.path), err)
		}
		index++
	}
	events := make([]Envelope, 0, limit)
	for len(events) < limit {
		if err := ctx.Err(); err != nil {
			return nil, index, err
		}
		event, _, err := readRecord(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, index, fmt.Errorf("read event record from %s: %w", filepath.Base(segment.path), err)
		}
		events = append(events, event)
		index++
	}
	return events, index, nil
}

func (s *FileSpool) Commit(ctx context.Context, checkpoint Checkpoint) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.readMu.Lock()
	defer s.readMu.Unlock()

	s.mu.RLock()
	current := s.checkpoint
	started, closed := s.started, s.closed
	s.mu.RUnlock()
	if !started {
		return ErrSpoolNotStarted
	}
	if closed {
		return ErrSpoolClosed
	}
	if checkpointLess(checkpoint, current) {
		return fmt.Errorf("checkpoint regression from %+v to %+v", current, checkpoint)
	}

	s.segmentMu.RLock()
	err := s.validateCheckpoint(checkpoint)
	s.segmentMu.RUnlock()
	if err != nil {
		return err
	}
	if err := writeCheckpoint(s.cfg.Directory, checkpoint); err != nil {
		return err
	}
	s.mu.Lock()
	s.checkpoint = checkpoint
	s.mu.Unlock()
	return s.cleanupCommittedSegments(checkpoint)
}

func (s *FileSpool) validateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.Segment == 0 {
		if checkpoint.Block != 0 || checkpoint.Record != 0 {
			return errors.New("zero segment checkpoint cannot contain a block or record offset")
		}
		return nil
	}
	segments, err := listSegments(s.cfg.Directory)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if segment.sequence != checkpoint.Segment {
			continue
		}
		if _, _, err := inspectSegment(segment.path, segment.size, segment.active); err != nil {
			return err
		}
		if segment.codec == CompressionZstd {
			_, _, err := compressedCheckpointRecordOffset(segment.path, checkpoint)
			return err
		}
		if checkpoint.Block != 0 {
			return fmt.Errorf("raw segment %d cannot use block checkpoint %d", checkpoint.Segment, checkpoint.Block)
		}
		records, _, err := inspectSegment(segment.path, segment.size, segment.active)
		if err != nil {
			return err
		}
		if checkpoint.Record > records {
			return fmt.Errorf("checkpoint record %d exceeds segment %d record count %d", checkpoint.Record, checkpoint.Segment, records)
		}
		return nil
	}
	return fmt.Errorf("checkpoint segment %d does not exist", checkpoint.Segment)
}

func (s *FileSpool) cleanupCommittedSegments(checkpoint Checkpoint) error {
	return s.cleanupCommittedSegmentsMode(checkpoint, false)
}

func segmentCheckpointRecordOffset(segment segmentFile, checkpoint Checkpoint) (uint64, uint64, error) {
	if segment.codec == CompressionZstd {
		return compressedCheckpointRecordOffset(segment.path, checkpoint)
	}
	if checkpoint.Block != 0 {
		return 0, 0, fmt.Errorf("raw segment %d cannot use block checkpoint %d", segment.sequence, checkpoint.Block)
	}
	records, _, err := inspectSegment(segment.path, segment.size, segment.active)
	if err != nil {
		return 0, 0, err
	}
	if checkpoint.Record > records {
		return 0, records, fmt.Errorf("checkpoint record %d exceeds segment %d record count %d", checkpoint.Record, segment.sequence, records)
	}
	return checkpoint.Record, records, nil
}

func (s *FileSpool) Status() Status {
	status := Status{Driver: DriverFile}
	s.mu.RLock()
	started := s.started
	checkpoint := s.checkpoint
	directory := s.cfg.Directory
	s.mu.RUnlock()
	if !started {
		return status
	}

	s.segmentMu.RLock()
	segments, err := listSegments(directory)
	if err == nil {
		status.Segments = len(segments)
		for _, segment := range segments {
			if checkpoint.Segment != 0 && segment.sequence < checkpoint.Segment {
				continue
			}
			records, _, inspectErr := inspectSegment(segment.path, segment.size, segment.active)
			if inspectErr != nil {
				continue
			}
			pending := records
			if segment.sequence == checkpoint.Segment {
				consumed, total, offsetErr := segmentCheckpointRecordOffset(segment, checkpoint)
				if offsetErr != nil {
					continue
				}
				if consumed >= total {
					pending = 0
				} else {
					pending = total - consumed
				}
			}
			status.PendingEvents += int64(pending)
			status.PendingBytes += segment.size
			info, statErr := os.Stat(segment.path)
			if statErr == nil && (status.OldestEventAt == nil || info.ModTime().Before(*status.OldestEventAt)) {
				modified := info.ModTime()
				status.OldestEventAt = &modified
			}
		}
	}
	s.segmentMu.RUnlock()

	if storage, storageErr := s.inspectStorage(0); storageErr == nil {
		status.StorageBytes = storage.contentBytes
		status.DiskFreeBytes = storage.freeBytes
		status.StorageUsageRatio = storage.usageRatio
		status.Warning = storage.level >= storagePressureWarning
		status.Compact = storage.level >= storagePressureCompact
		status.Emergency = storage.level >= storagePressureEmergency
		status.EmergencyReserveAvailable = storage.reserveAvailable
	}
	return status
}

func loadCheckpoint(directory string) (Checkpoint, error) {
	path := filepath.Join(directory, checkpointFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Checkpoint{}, nil
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("read spool checkpoint: %w", err)
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("decode spool checkpoint: %w", err)
	}
	return checkpoint, nil
}

func writeCheckpoint(directory string, checkpoint Checkpoint) error {
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".checkpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary checkpoint: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write spool checkpoint: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync spool checkpoint: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, checkpointFileName)); err != nil {
		return fmt.Errorf("replace spool checkpoint: %w", err)
	}
	return syncDirectory(directory)
}

func checkpointLess(left, right Checkpoint) bool {
	if left.Segment != right.Segment {
		return left.Segment < right.Segment
	}
	if left.Block != right.Block {
		return left.Block < right.Block
	}
	return left.Record < right.Record
}

func oldestTime(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.Before(*current) {
		value := candidate
		return &value
	}
	return current
}
