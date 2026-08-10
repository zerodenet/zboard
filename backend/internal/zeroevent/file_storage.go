package zeroevent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	emergencyReserveFileName   = ".emergency-reserve"
	storageMaintenanceInterval = 30 * time.Second
)

type storagePressureLevel uint8

const (
	storagePressureNormal storagePressureLevel = iota
	storagePressureWarning
	storagePressureCompact
	storagePressureEmergency
)

type storageSnapshot struct {
	contentBytes     int64
	freeBytes        int64
	usageRatio       float64
	level            storagePressureLevel
	reserveAvailable bool
}

func (s *FileSpool) storageLoop() {
	defer close(s.storageDone)
	ticker := time.NewTicker(storageMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.storageCh:
			s.maintainStorage()
		case <-ticker.C:
			s.maintainStorage()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *FileSpool) requestStorageMaintenance() {
	select {
	case s.storageCh <- struct{}{}:
	default:
	}
}

func (s *FileSpool) maintainStorage() {
	snapshot, err := s.inspectStorage(0)
	if err != nil {
		return
	}
	switch {
	case snapshot.level >= storagePressureEmergency:
		s.cleanupCommittedForPressure(true)
		_, _ = s.Compact(s.ctx)
		s.requestCompression()
		_ = s.releaseEmergencyReserve()
	case snapshot.level >= storagePressureCompact:
		s.cleanupCommittedForPressure(true)
		_, _ = s.Compact(s.ctx)
		s.requestCompression()
	case snapshot.level >= storagePressureWarning:
		s.cleanupCommittedForPressure(false)
	case !snapshot.reserveAvailable:
		// A normal pressure level already guarantees enough configured free-space
		// headroom for min-free + emergency + critical reserves. Recreate the
		// physical emergency reserve only from this background worker.
		_ = s.ensureEmergencyReserve()
	}
}

func (s *FileSpool) prepareAppend(requiredBytes int64) {
	snapshot, err := s.inspectStorage(requiredBytes)
	if err != nil {
		return
	}

	if snapshot.level >= storagePressureWarning {
		s.cleanupCommittedForPressure(snapshot.level >= storagePressureCompact)
	}
	if snapshot.level >= storagePressureCompact {
		// Semantic compaction is intentionally performed by the storage worker,
		// never inline with the append request. This keeps HTTP durability latency
		// independent from segment rewrite CPU/disk work.
		s.requestStorageMaintenance()
	}

	// Re-check after cheap reclamation. Soft thresholds never reject an append;
	// they only increase reclamation pressure. The emergency reserve is released
	// only after the spool is genuinely in its emergency band or filesystem free
	// space would fall below the configured floor.
	snapshot, err = s.inspectStorage(requiredBytes)
	if err == nil && snapshot.level >= storagePressureEmergency {
		_ = s.releaseEmergencyReserve()
	}
	// Let the background worker restore a previously released reserve once
	// pressure returns to normal; never rebuild it inline with HTTP append.
	s.requestStorageMaintenance()
}

func (s *FileSpool) emergencyReclaim() {
	_ = s.releaseEmergencyReserve()
	s.cleanupCommittedForPressure(true)
	// A real ENOSPC/EDQUOT remains the narrow synchronous failure path. Semantic
	// compaction stays off the writer path because ReadBatch obtains its durable
	// active snapshot through the writer goroutine; waiting for readMu here could
	// create lock inversion. Sealed compression is safe and still retried inline.
	_ = s.compressReadySegments()
}

func (s *FileSpool) cleanupCommittedForPressure(ignoreRetention bool) {
	s.mu.RLock()
	checkpoint := s.checkpoint
	s.mu.RUnlock()
	if checkpoint.Segment == 0 {
		return
	}
	_ = s.cleanupCommittedSegmentsMode(checkpoint, ignoreRetention)
}

func (s *FileSpool) inspectStorage(requiredBytes int64) (storageSnapshot, error) {
	if requiredBytes < 0 {
		requiredBytes = 0
	}
	contentBytes, reserveAvailable, err := storageDirectoryBytes(s.cfg.Directory)
	if err != nil {
		return storageSnapshot{}, err
	}
	freeBytes, err := filesystemFreeBytes(s.cfg.Directory)
	if err != nil {
		return storageSnapshot{}, err
	}
	pressureConfig := s.cfg.Storage
	if reserveAvailable {
		// statfs already reports free space after the preallocated reserve consumed
		// its blocks. Only add EmergencyReserve to free-space headroom while the
		// reserve is absent and needs to be recreated.
		pressureConfig.EmergencyReserve = 0
	}
	level, ratio := evaluateStoragePressure(pressureConfig, contentBytes, freeBytes, requiredBytes)
	return storageSnapshot{
		contentBytes:     contentBytes,
		freeBytes:        freeBytes,
		usageRatio:       ratio,
		level:            level,
		reserveAvailable: reserveAvailable,
	}, nil
}

func evaluateStoragePressure(cfg StorageConfig, contentBytes, freeBytes, requiredBytes int64) (storagePressureLevel, float64) {
	projected := contentBytes + requiredBytes
	if projected < 0 {
		projected = int64(^uint64(0) >> 1)
	}
	ratio := 0.0
	if cfg.MaxSize > 0 {
		ratio = float64(projected) / float64(cfg.MaxSize)
	}
	level := storagePressureNormal
	if ratio >= cfg.WarningRatio {
		level = storagePressureWarning
	}
	if ratio >= cfg.CompactRatio {
		level = storagePressureCompact
	}
	if ratio >= cfg.EmergencyRatio || (cfg.MaxSize > 0 && projected >= cfg.MaxSize) {
		level = storagePressureEmergency
	}

	// CriticalReserve is deliberately a reclamation headroom rather than a
	// rejection threshold. Semantic compaction may reclaim superseded cumulative
	// snapshots in this band, but crossing it still never rejects an event.
	if cfg.MaxSize > 0 && cfg.CriticalReserve > 0 {
		criticalBoundary := cfg.MaxSize - cfg.CriticalReserve
		if criticalBoundary < 0 {
			criticalBoundary = 0
		}
		if projected >= criticalBoundary && level < storagePressureCompact {
			level = storagePressureCompact
		}
	}

	freeAfter := freeBytes - requiredBytes
	if freeAfter < 0 {
		freeAfter = 0
	}
	if cfg.MinFreeSpace > 0 {
		if freeAfter < cfg.MinFreeSpace {
			level = storagePressureEmergency
		} else if freeAfter < cfg.MinFreeSpace+cfg.EmergencyReserve && level < storagePressureCompact {
			level = storagePressureCompact
		} else if freeAfter < cfg.MinFreeSpace+cfg.EmergencyReserve+cfg.CriticalReserve && level < storagePressureWarning {
			level = storagePressureWarning
		}
	}
	return level, ratio
}

func storageDirectoryBytes(directory string) (int64, bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, false, err
	}
	var total int64
	reserveAvailable := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, false, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if entry.Name() == emergencyReserveFileName {
			reserveAvailable = info.Size() > 0
			continue
		}
		// Count every non-reserve regular file, including temporary or duplicate
		// recovery artifacts. Pressure decisions should reflect actual directory
		// occupancy rather than only the logical segment set returned by listSegments.
		if info.Size() > 0 && total > int64(^uint64(0)>>1)-info.Size() {
			total = int64(^uint64(0) >> 1)
			continue
		}
		total += info.Size()
	}
	return total, reserveAvailable, nil
}

func (s *FileSpool) ensureEmergencyReserve() error {
	if s.cfg.Storage.EmergencyReserve <= 0 {
		return nil
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	return ensureReserveFile(s.cfg.Directory, s.cfg.Storage.EmergencyReserve)
}

func (s *FileSpool) releaseEmergencyReserve() error {
	if s.cfg.Storage.EmergencyReserve <= 0 {
		return nil
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	path := filepath.Join(s.cfg.Directory, emergencyReserveFileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release emergency reserve: %w", err)
	}
	return syncDirectory(s.cfg.Directory)
}

func ensureReserveFile(directory string, size int64) error {
	path := filepath.Join(directory, emergencyReserveFileName)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == size {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o640)
	if err != nil {
		return fmt.Errorf("create emergency reserve: %w", err)
	}
	removeOnError := true
	defer func() {
		_ = file.Close()
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	if err := preallocateReserve(file, size); err != nil {
		return fmt.Errorf("preallocate emergency reserve: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync emergency reserve: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	removeOnError = false
	return syncDirectory(directory)
}

func zeroFillReserve(file *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	chunk := make([]byte, 1<<20)
	remaining := size
	for remaining > 0 {
		writeSize := int64(len(chunk))
		if remaining < writeSize {
			writeSize = remaining
		}
		if _, err := file.Write(chunk[:writeSize]); err != nil {
			return err
		}
		remaining -= writeSize
	}
	return nil
}

func (s *FileSpool) writeRecordWithCapacityRecovery(record []byte) error {
	if s.active == nil {
		return errors.New("cannot append without an active segment")
	}
	for attempt := 0; attempt < 2; attempt++ {
		startSize := s.active.size
		written, err := s.active.file.Write(record)
		if err == nil && written == len(record) {
			s.active.size += int64(written)
			s.active.records++
			return nil
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		_ = s.active.file.Truncate(startSize)
		_, _ = s.active.file.Seek(startSize, io.SeekStart)
		if attempt == 0 && isStorageCapacityError(err) {
			s.emergencyReclaim()
			continue
		}
		return fmt.Errorf("append event record: %w", err)
	}
	return errors.New("append event record failed after capacity recovery")
}

func (s *FileSpool) syncActiveWithCapacityRecovery() error {
	if s.active == nil {
		return nil
	}
	if err := s.active.file.Sync(); err != nil {
		if !isStorageCapacityError(err) {
			return fmt.Errorf("sync event segment: %w", err)
		}
		s.emergencyReclaim()
		if retryErr := s.active.file.Sync(); retryErr != nil {
			return fmt.Errorf("sync event segment after capacity recovery: %w", retryErr)
		}
	}
	return nil
}

func isStorageCapacityError(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
