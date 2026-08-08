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

	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return Batch{}, err
	}
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
			segments = append(segments, segmentFile{sequence: snapshot.sequence, path: snapshot.path, active: true, size: snapshot.size})
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
		startRecord := uint64(0)
		if segment.sequence == checkpoint.Segment {
			startRecord = checkpoint.Record
		}
		events, nextRecord, err := readSegmentBatch(ctx, segment, startRecord, limit-len(batch.Events))
		if err != nil {
			return Batch{}, err
		}
		if len(events) == 0 {
			continue
		}
		batch.Events = append(batch.Events, events...)
		batch.Next = Checkpoint{Segment: segment.sequence, Record: nextRecord}
	}
	return batch, nil
}

func readSegmentBatch(ctx context.Context, segment segmentFile, startRecord uint64, limit int) ([]Envelope, uint64, error) {
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
	if err := s.validateCheckpoint(checkpoint); err != nil {
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
	if checkpoint.Block != 0 {
		return errors.New("file spool checkpoint block must be zero before compressed blocks are enabled")
	}
	if checkpoint.Segment == 0 {
		if checkpoint.Record != 0 {
			return errors.New("zero segment checkpoint cannot contain a record offset")
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
	segments, err := listSegments(s.cfg.Directory)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if segment.active || segment.sequence > checkpoint.Segment {
			continue
		}
		deleteSegment := segment.sequence < checkpoint.Segment
		if segment.sequence == checkpoint.Segment {
			records, _, err := inspectSegment(segment.path, segment.size, false)
			if err != nil {
				return err
			}
			deleteSegment = checkpoint.Record >= records
		}
		if deleteSegment && s.cfg.Retention.Consumed > 0 {
			info, err := os.Stat(segment.path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil && time.Since(info.ModTime()) < s.cfg.Retention.Consumed {
				deleteSegment = false
			}
		}
		if deleteSegment {
			if err := os.Remove(segment.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove committed segment %s: %w", filepath.Base(segment.path), err)
			}
		}
	}
	return syncDirectory(s.cfg.Directory)
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
	segments, err := listSegments(directory)
	if err != nil {
		return status
	}
	status.Segments = len(segments)
	for _, segment := range segments {
		if checkpoint.Segment != 0 && segment.sequence < checkpoint.Segment {
			continue
		}
		records, _, err := inspectSegment(segment.path, segment.size, segment.active)
		if err != nil {
			continue
		}
		if segment.sequence == checkpoint.Segment && checkpoint.Record < records {
			records -= checkpoint.Record
		} else if segment.sequence == checkpoint.Segment {
			records = 0
		}
		status.PendingEvents += int64(records)
		status.PendingBytes += segment.size
		info, err := os.Stat(segment.path)
		if err == nil && (status.OldestEventAt == nil || info.ModTime().Before(*status.OldestEventAt)) {
			modified := info.ModTime()
			status.OldestEventAt = &modified
		}
	}
	if s.cfg.Storage.MaxSize > 0 {
		status.Emergency = float64(status.PendingBytes)/float64(s.cfg.Storage.MaxSize) >= s.cfg.Storage.EmergencyRatio
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
