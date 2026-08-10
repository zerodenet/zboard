package zeroevent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s *FileSpool) cleanupCommittedSegmentsMode(checkpoint Checkpoint, ignoreRetention bool) error {
	s.segmentMu.Lock()
	defer s.segmentMu.Unlock()

	segments, err := listSegments(s.cfg.Directory)
	if err != nil {
		return err
	}
	changed := false
	for _, segment := range segments {
		if segment.active || segment.sequence > checkpoint.Segment {
			continue
		}
		deleteSegment := segment.sequence < checkpoint.Segment
		if segment.sequence == checkpoint.Segment {
			consumed, total, err := segmentCheckpointRecordOffset(segment, checkpoint)
			if err != nil {
				return err
			}
			deleteSegment = consumed >= total
		}
		if deleteSegment && !ignoreRetention && s.cfg.Retention.Consumed > 0 {
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
			changed = true
		}
	}
	if changed {
		return syncDirectory(s.cfg.Directory)
	}
	return nil
}
