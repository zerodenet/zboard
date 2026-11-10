package zeroevent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sequenceRecoveryJournal = ".sequence-recovery.json"
const sequenceRecoveryReceipt = ".sequence-recovery-last.json"
const maxSequenceRecoveryJournal = 16 << 20

type sequenceRecoveryMove struct {
	From, To string
	Size     int64
	Modified int64
}
type sequenceRecoveryPlan struct {
	Version    int
	Checkpoint Checkpoint
	Moves      []sequenceRecoveryMove
}
type sequenceRecoveryResult struct {
	Version                   int
	PreviousCheckpoint        Checkpoint
	FirstSegment, LastSegment uint64
	Segments                  int
	CompletedAt               time.Time
}

// Called under exclusive kernel spool ownership, before any writer/reader or
// maintenance worker starts. Older writers could reuse sequence 1 after the
// last sealed file was reclaimed, leaving new acknowledged events behind an
// older durable checkpoint. Rebase that whole unambiguous tail above the old
// cursor; never reset the cursor or alter an envelope, ID, proof or payload.
// Projection's existing durable idempotency remains authoritative on replay.
func reconcileSegmentSequence(directory string, checkpoint Checkpoint) error {
	path := filepath.Join(directory, sequenceRecoveryJournal)
	data, err := readRecoveryJournal(path)
	var plan sequenceRecoveryPlan
	if err == nil {
		if err := json.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("decode event sequence recovery journal: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		segments, err := listSegments(directory)
		if err != nil {
			return err
		}
		if len(segments) == 0 || checkpoint.Segment == 0 || segments[len(segments)-1].sequence >= checkpoint.Segment {
			return nil
		}
		if uint64(len(segments)) > math.MaxUint64-checkpoint.Segment {
			return errors.New("event sequence recovery exhausted; files retained")
		}
		plan = sequenceRecoveryPlan{Version: 1, Checkpoint: checkpoint}
		for i, segment := range segments {
			info, err := os.Lstat(segment.path)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("event sequence recovery requires regular files")
			}
			suffix := segmentReadySuffix
			if segment.active {
				suffix = segmentActiveSuffix
			} else if segment.codec == CompressionZstd {
				suffix = segmentZstdSuffix
			} else if segment.codec == CompressionLZ4 {
				suffix = segmentLZ4Suffix
			}
			plan.Moves = append(plan.Moves, sequenceRecoveryMove{From: filepath.Base(segment.path), To: filepath.Base(segmentPath(directory, checkpoint.Segment+uint64(i)+1, suffix)), Size: info.Size(), Modified: info.ModTime().UnixNano()})
		}
		if err := validateSequenceRecovery(checkpoint, plan); err != nil {
			return err
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		if len(encoded) > maxSequenceRecoveryJournal {
			return errors.New("event sequence recovery journal exceeds safety limit; files retained")
		}
		if err := writeRecoveryMetadata(directory, sequenceRecoveryJournal, encoded); err != nil {
			return err
		}
	}
	return finishSequenceRecovery(directory, checkpoint, plan)
}
func readRecoveryJournal(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSequenceRecoveryJournal+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSequenceRecoveryJournal {
		return nil, errors.New("event sequence recovery journal exceeds safety limit")
	}
	return data, nil
}
func validateSequenceRecovery(checkpoint Checkpoint, plan sequenceRecoveryPlan) error {
	if plan.Version != 1 || plan.Checkpoint != checkpoint || checkpoint.Segment == 0 || len(plan.Moves) == 0 || uint64(len(plan.Moves)) > math.MaxUint64-checkpoint.Segment {
		return errors.New("event sequence recovery journal conflicts with checkpoint; files retained")
	}
	var previous uint64
	for i, move := range plan.Moves {
		source, active, codec, ok := parseSegmentName(move.From)
		target, targetActive, targetCodec, targetOK := parseSegmentName(move.To)
		if !ok || !targetOK || filepath.Base(move.From) != move.From || filepath.Base(move.To) != move.To || strings.ContainsAny(move.From+move.To, "/\\") || source == 0 || source >= checkpoint.Segment || source <= previous || target != checkpoint.Segment+uint64(i)+1 || active != targetActive || codec != targetCodec || move.Size < 0 {
			return errors.New("invalid event sequence recovery mapping; files retained")
		}
		if active && i != len(plan.Moves)-1 {
			return errors.New("multiple or non-tail active files in event sequence recovery; files retained")
		}
		previous = source
	}
	return nil
}
func recoveryFileMatches(path string, move sequenceRecoveryMove) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != move.Size || info.ModTime().UnixNano() != move.Modified {
		return false, errors.New("event sequence recovery file changed; files retained")
	}
	return true, nil
}
func finishSequenceRecovery(directory string, checkpoint Checkpoint, plan sequenceRecoveryPlan) error {
	if err := validateSequenceRecovery(checkpoint, plan); err != nil {
		return err
	}
	// Check the complete mapping before mutating any filename. A resumed journal
	// may have either source or destination, never both and never neither.
	for _, move := range plan.Moves {
		from, err := recoveryFileMatches(filepath.Join(directory, move.From), move)
		if err != nil {
			return err
		}
		to, err := recoveryFileMatches(filepath.Join(directory, move.To), move)
		if err != nil {
			return err
		}
		if from == to {
			return errors.New("event sequence recovery has missing or conflicting files; files retained")
		}
	}
	for _, move := range plan.Moves {
		from := filepath.Join(directory, move.From)
		exists, err := recoveryFileMatches(from, move)
		if err != nil {
			return err
		}
		if exists {
			if err := os.Rename(from, filepath.Join(directory, move.To)); err != nil {
				return fmt.Errorf("rebase event sequence: %w", err)
			}
		}
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	receipt, err := json.Marshal(sequenceRecoveryResult{Version: 1, PreviousCheckpoint: checkpoint, FirstSegment: checkpoint.Segment + 1, LastSegment: checkpoint.Segment + uint64(len(plan.Moves)), Segments: len(plan.Moves), CompletedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := writeRecoveryMetadata(directory, sequenceRecoveryReceipt, receipt); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, sequenceRecoveryJournal)); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	log.Printf("Zero event sequence recovery completed: segments=%d previous_checkpoint=%d new_first=%d new_last=%d original_event_ids=preserved", len(plan.Moves), checkpoint.Segment, checkpoint.Segment+1, checkpoint.Segment+uint64(len(plan.Moves)))
	return nil
}
func writeRecoveryMetadata(directory, name string, data []byte) error {
	f, err := os.CreateTemp(directory, ".sequence-recovery-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err := f.Chmod(0o640); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, filepath.Join(directory, name)); err != nil {
		return err
	}
	return syncDirectory(directory)
}
