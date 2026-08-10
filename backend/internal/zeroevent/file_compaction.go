package zeroevent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type semanticCompactionKind uint8

const (
	semanticCompactionFlowUpdate semanticCompactionKind = iota + 1
	semanticCompactionNodeStats
)

type semanticCompactionKey struct {
	kind           semanticCompactionKind
	nodeID         uint64
	coreInstanceID string
	flowID         string
}

type semanticCompactionLatest struct {
	index    int
	sequence uint64
}

func (s *FileSpool) Compact(ctx context.Context) (result CompactionResult, err error) {
	if !s.cfg.Compaction.Enabled {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	// Compact shares readMu with ReadBatch/Commit. An in-flight read horizon is
	// remembered after ReadBatch returns, so even while the caller is projecting
	// a batch we only rewrite segments strictly beyond every event already read.
	s.readMu.Lock()
	defer s.readMu.Unlock()

	s.mu.RLock()
	started, closed := s.started, s.closed
	checkpoint := s.checkpoint
	s.mu.RUnlock()
	if !started {
		return result, ErrSpoolNotStarted
	}
	if closed {
		return result, ErrSpoolClosed
	}

	// Record the run after the lifecycle checks. The closure observes the final
	// named result, so if an early segment was already published before a later
	// segment fails, those completed savings remain visible in Status().
	defer func() { s.recordCompactionResult(result) }()

	horizon := checkpoint.Segment
	if s.hasInflight && s.inflightNext.Segment > horizon {
		horizon = s.inflightNext.Segment
	}

	// The compression worker and semantic compactor both replace sealed segment
	// representations. Serializing them avoids stale .ready/.ready.zst takeover.
	s.compressionMu.Lock()
	defer s.compressionMu.Unlock()

	s.segmentMu.RLock()
	segments, err := listSegments(s.cfg.Directory)
	s.segmentMu.RUnlock()
	if err != nil {
		return result, err
	}

	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if segment.active || segment.sequence <= horizon {
			continue
		}
		compacted, err := s.compactSealedSegment(ctx, segment)
		if err != nil {
			return result, err
		}
		result.Segments += compacted.Segments
		result.EventsBefore += compacted.EventsBefore
		result.EventsAfter += compacted.EventsAfter
		result.FlowUpdatesSaved += compacted.FlowUpdatesSaved
		result.NodeStatsSaved += compacted.NodeStatsSaved
		result.BytesBefore += compacted.BytesBefore
		result.BytesAfter += compacted.BytesAfter
		result.UnsafeSkipped += compacted.UnsafeSkipped
	}
	if result.Segments > 0 {
		s.requestCompression()
	}
	return result, nil
}

func (s *FileSpool) recordCompactionResult(result CompactionResult) {
	s.compactionStatsMu.Lock()
	defer s.compactionStatsMu.Unlock()
	s.compactionRuns++
	s.compactionTotals.Segments += result.Segments
	s.compactionTotals.EventsBefore += result.EventsBefore
	s.compactionTotals.EventsAfter += result.EventsAfter
	s.compactionTotals.FlowUpdatesSaved += result.FlowUpdatesSaved
	s.compactionTotals.NodeStatsSaved += result.NodeStatsSaved
	s.compactionTotals.BytesBefore += result.BytesBefore
	s.compactionTotals.BytesAfter += result.BytesAfter
	s.compactionTotals.UnsafeSkipped += result.UnsafeSkipped
}

func (s *FileSpool) compactionStatus() (int64, CompactionResult) {
	s.compactionStatsMu.RLock()
	defer s.compactionStatsMu.RUnlock()
	return s.compactionRuns, s.compactionTotals
}

func (s *FileSpool) compactSealedSegment(ctx context.Context, segment segmentFile) (CompactionResult, error) {
	var result CompactionResult
	events, err := readWholeSegment(ctx, segment)
	if err != nil {
		return result, err
	}
	if len(events) < 2 {
		return result, nil
	}

	survivors, flowSaved, statsSaved, unsafeSkipped := compactSemanticEvents(events, s.cfg.Compaction)
	result.UnsafeSkipped = unsafeSkipped
	if len(survivors) == len(events) {
		return result, nil
	}

	temporary, err := os.CreateTemp(s.cfg.Directory, fmt.Sprintf(".%020d-compact-*.tmp", segment.sequence))
	if err != nil {
		return result, fmt.Errorf("create compacted segment temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return result, err
	}
	if err := s.writeCompactedSegment(temporary, segment.codec, survivors); err != nil {
		temporary.Close()
		return result, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return result, fmt.Errorf("sync compacted segment: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return result, err
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		return result, err
	}
	if err := validateCompactedRepresentation(temporaryPath, segment.codec, uint64(len(survivors)), info.Size()); err != nil {
		return result, err
	}

	// The read horizon cannot move while Compact holds readMu. Commit therefore
	// cannot make this segment current between planning and atomic publication.
	s.segmentMu.Lock()
	defer s.segmentMu.Unlock()
	if _, err := os.Stat(segment.path); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	if err := os.Rename(temporaryPath, segment.path); err != nil {
		return result, fmt.Errorf("publish compacted segment %s: %w", filepath.Base(segment.path), err)
	}
	if segment.codec == CompressionNone {
		// A crash during an earlier representation switch can leave a valid zstd
		// duplicate. The newly compacted raw file is authoritative; remove the old
		// compressed duplicate before allowing the compression worker to run again.
		duplicate := segmentPath(s.cfg.Directory, segment.sequence, segmentZstdSuffix)
		if err := os.Remove(duplicate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("remove stale compressed duplicate: %w", err)
		}
	}
	if err := syncDirectory(s.cfg.Directory); err != nil {
		return result, err
	}

	result.Segments = 1
	result.EventsBefore = int64(len(events))
	result.EventsAfter = int64(len(survivors))
	result.FlowUpdatesSaved = flowSaved
	result.NodeStatsSaved = statsSaved
	result.BytesBefore = segment.size
	result.BytesAfter = info.Size()
	return result, nil
}

func readWholeSegment(ctx context.Context, segment segmentFile) ([]Envelope, error) {
	records, _, err := inspectSegment(segment.path, segment.size, false)
	if err != nil {
		return nil, err
	}
	if records == 0 {
		return nil, nil
	}
	if records > uint64(math.MaxInt) {
		return nil, errors.New("segment record count exceeds compaction limit")
	}
	events, _, err := readSegmentBatch(ctx, segment, Checkpoint{Segment: segment.sequence}, int(records))
	if err != nil {
		return nil, err
	}
	if uint64(len(events)) != records {
		return nil, fmt.Errorf("read compactable segment %d: got %d records, want %d", segment.sequence, len(events), records)
	}
	return events, nil
}

func (s *FileSpool) writeCompactedSegment(target *os.File, codec string, events []Envelope) error {
	if codec == CompressionZstd {
		var raw bytes.Buffer
		for _, event := range events {
			record, err := encodeRecord(event)
			if err != nil {
				return err
			}
			if _, err := raw.Write(record); err != nil {
				return err
			}
		}
		compression := s.cfg.Compression
		defaults := DefaultConfig().Compression
		if compression.BlockSize <= 0 {
			compression.BlockSize = defaults.BlockSize
		}
		if compression.Workers <= 0 {
			compression.Workers = defaults.Workers
		}
		if compression.Level == 0 {
			compression.Level = defaults.Level
		}
		return compressRawSegmentToZstdWithWorkers(bytes.NewReader(raw.Bytes()), target, compression.BlockSize, compression.Level, compression.Workers)
	}
	for _, event := range events {
		record, err := encodeRecord(event)
		if err != nil {
			return err
		}
		if _, err := target.Write(record); err != nil {
			return fmt.Errorf("write compacted event record: %w", err)
		}
	}
	return nil
}

func validateCompactedRepresentation(path, codec string, records uint64, size int64) error {
	var actual uint64
	var err error
	if codec == CompressionZstd {
		actual, _, err = inspectCompressedSegment(path)
	} else {
		actual, _, err = inspectRawSegment(path, size, false)
	}
	if err != nil {
		return fmt.Errorf("validate compacted segment: %w", err)
	}
	if actual != records {
		return fmt.Errorf("compacted segment record count = %d, want %d", actual, records)
	}
	return nil
}

func compactSemanticEvents(events []Envelope, cfg CompactionConfig) ([]Envelope, int64, int64, int64) {
	latest := make(map[semanticCompactionKey]semanticCompactionLatest)
	conflicted := make(map[semanticCompactionKey]bool)
	keys := make([]semanticCompactionKey, len(events))
	keyed := make([]bool, len(events))
	var unsafeSkipped int64

	for index, event := range events {
		key, replaceable, safe := semanticKey(event, cfg)
		if replaceable && !safe {
			unsafeSkipped++
		}
		if !safe {
			continue
		}
		keys[index] = key
		keyed[index] = true
		current, exists := latest[key]
		if !exists || event.Sequence > current.sequence {
			latest[key] = semanticCompactionLatest{index: index, sequence: event.Sequence}
			continue
		}
		if event.Sequence == current.sequence {
			// A core sequence is unique inside one core_instance_id. Seeing the same
			// key/sequence twice can be an HTTP replay; keep all copies rather than
			// hiding a theoretically inconsistent duplicate during compaction.
			conflicted[key] = true
		}
	}

	survivors := make([]Envelope, 0, len(events))
	var flowSaved, statsSaved int64
	for index, event := range events {
		if !keyed[index] {
			survivors = append(survivors, event)
			continue
		}
		key := keys[index]
		if conflicted[key] || latest[key].index == index {
			survivors = append(survivors, event)
			continue
		}
		switch key.kind {
		case semanticCompactionFlowUpdate:
			flowSaved++
		case semanticCompactionNodeStats:
			statsSaved++
		}
	}
	return survivors, flowSaved, statsSaved, unsafeSkipped
}

// semanticKey intentionally requires both core_instance_id and global event
// sequence. Older buffered records without runtime identity remain untouched;
// flow_id reuse across a restart must never be guessed across generations.
func semanticKey(event Envelope, cfg CompactionConfig) (semanticCompactionKey, bool, bool) {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	if event.NodeID == 0 || instanceID == "" || event.Sequence == 0 {
		switch event.Type {
		case "flow.updated":
			return semanticCompactionKey{}, cfg.MergeFlowUpdates, false
		case "stats.sampled":
			return semanticCompactionKey{}, cfg.MergeNodeStats, false
		default:
			return semanticCompactionKey{}, false, false
		}
	}
	switch event.Type {
	case "flow.updated":
		if !cfg.MergeFlowUpdates {
			return semanticCompactionKey{}, false, false
		}
		flowID := strings.TrimSpace(event.FlowID)
		if flowID == "" {
			return semanticCompactionKey{}, true, false
		}
		return semanticCompactionKey{
			kind:           semanticCompactionFlowUpdate,
			nodeID:         event.NodeID,
			coreInstanceID: instanceID,
			flowID:         flowID,
		}, true, true
	case "stats.sampled":
		if !cfg.MergeNodeStats {
			return semanticCompactionKey{}, false, false
		}
		return semanticCompactionKey{
			kind:           semanticCompactionNodeStats,
			nodeID:         event.NodeID,
			coreInstanceID: instanceID,
		}, true, true
	default:
		return semanticCompactionKey{}, false, false
	}
}
