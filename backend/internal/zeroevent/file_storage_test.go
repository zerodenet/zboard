package zeroevent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvaluateStoragePressureUsesRatiosFreeSpaceAndCriticalHeadroom(t *testing.T) {
	cfg := StorageConfig{
		MaxSize:          1000,
		WarningRatio:     0.50,
		CompactRatio:     0.80,
		EmergencyRatio:   0.95,
		MinFreeSpace:     100,
		EmergencyReserve: 50,
		CriticalReserve:  400,
	}

	level, ratio := evaluateStoragePressure(cfg, 400, 1000, 0)
	if level != storagePressureNormal || ratio != 0.4 {
		t.Fatalf("normal pressure = level %d ratio %.2f", level, ratio)
	}

	level, _ = evaluateStoragePressure(cfg, 550, 1000, 0)
	if level != storagePressureWarning {
		t.Fatalf("warning pressure = %d, want %d", level, storagePressureWarning)
	}

	// 650 bytes is below the configured compact ratio, but it has entered the
	// 400-byte critical headroom (1000 - 400), so reclamation must become compact.
	level, _ = evaluateStoragePressure(cfg, 650, 1000, 0)
	if level != storagePressureCompact {
		t.Fatalf("critical headroom pressure = %d, want %d", level, storagePressureCompact)
	}

	level, _ = evaluateStoragePressure(cfg, 400, 90, 0)
	if level != storagePressureEmergency {
		t.Fatalf("min free pressure = %d, want %d", level, storagePressureEmergency)
	}

	level, _ = evaluateStoragePressure(cfg, 990, 1000, 20)
	if level != storagePressureEmergency {
		t.Fatalf("max size pressure = %d, want %d", level, storagePressureEmergency)
	}
}

func TestEmergencyReserveLifecycle(t *testing.T) {
	directory := t.TempDir()
	cfg := DefaultConfig()
	cfg.Directory = directory
	cfg.Storage.EmergencyReserve = 64 << 10
	spool := &FileSpool{cfg: cfg}

	if err := spool.ensureEmergencyReserve(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, emergencyReserveFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != cfg.Storage.EmergencyReserve {
		t.Fatalf("reserve size = %d, want %d", info.Size(), cfg.Storage.EmergencyReserve)
	}
	if err := spool.releaseEmergencyReserve(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("reserve still exists after release: %v", err)
	}
}

func TestStorageMaintenanceRestoresReserveWhenPressureNormal(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Storage.EmergencyReserve = 64 << 10
	cfg.Storage.CriticalReserve = 0
	spool := &FileSpool{cfg: cfg}
	path := filepath.Join(cfg.Directory, emergencyReserveFileName)

	if err := spool.ensureEmergencyReserve(); err != nil {
		t.Fatal(err)
	}
	if err := spool.releaseEmergencyReserve(); err != nil {
		t.Fatal(err)
	}
	spool.maintainStorage()
	if info, err := os.Stat(path); err != nil || info.Size() != cfg.Storage.EmergencyReserve {
		t.Fatalf("maintenance did not restore reserve: info=%v err=%v", info, err)
	}
}

func TestStorageMaintenanceReleasesReserveUnderEmergencyPressure(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Storage.MaxSize = 1024
	cfg.Storage.WarningRatio = 0.50
	cfg.Storage.CompactRatio = 0.70
	cfg.Storage.EmergencyRatio = 0.90
	cfg.Storage.EmergencyReserve = 64 << 10
	cfg.Storage.CriticalReserve = 0
	spool := &FileSpool{cfg: cfg}
	path := filepath.Join(cfg.Directory, emergencyReserveFileName)

	if err := spool.ensureEmergencyReserve(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Directory, "pressure.bin"), make([]byte, 2048), 0o640); err != nil {
		t.Fatal(err)
	}
	spool.maintainStorage()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("maintenance retained reserve under emergency pressure: %v", err)
	}
}

func TestFileSpoolSoftMaxSizeDoesNotRejectAppend(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Storage.MaxSize = 512
	cfg.Storage.WarningRatio = 0.50
	cfg.Storage.CompactRatio = 0.70
	cfg.Storage.EmergencyRatio = 0.90
	cfg.Storage.MinFreeSpace = 0
	cfg.Storage.EmergencyReserve = 0
	cfg.Storage.CriticalReserve = 128
	cfg.Segment.MaxSize = 4096

	spool, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer spool.Close()

	for sequence := uint64(1); sequence <= 8; sequence++ {
		if err := spool.Append(context.Background(), testEnvelope("soft-max", sequence)); err != nil {
			t.Fatalf("soft max rejected sequence %d: %v", sequence, err)
		}
	}
	status := spool.Status()
	if status.StorageBytes <= cfg.Storage.MaxSize {
		t.Fatalf("test did not exceed soft max: storage=%d max=%d", status.StorageBytes, cfg.Storage.MaxSize)
	}
	if !status.Emergency {
		t.Fatalf("status should report emergency after exceeding soft max: %+v", status)
	}
}

func TestPressureCleanupCanBypassConsumedRetention(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Storage.EmergencyReserve = 0
	cfg.Retention.Consumed = time.Hour
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
	batch, err := spool.ReadBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Next.Segment != 1 {
		t.Fatalf("unexpected first batch: %+v", batch)
	}
	if err := spool.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}

	ready := segmentPath(cfg.Directory, 1, segmentReadySuffix)
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("normal retention unexpectedly removed segment: %v", err)
	}
	if err := spool.cleanupCommittedSegmentsMode(batch.Next, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ready); !os.IsNotExist(err) {
		t.Fatalf("pressure cleanup retained consumed segment: %v", err)
	}
}
