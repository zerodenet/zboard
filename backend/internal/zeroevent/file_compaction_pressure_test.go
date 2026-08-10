package zeroevent

import (
	"context"
	"testing"
)

func TestStorageMaintenanceCompactsSupersededSnapshotsUnderPressure(t *testing.T) {
	cfg := testFileConfig(t)
	cfg.Compaction.Enabled = true
	cfg.Storage.WarningRatio = 0.50
	cfg.Storage.CompactRatio = 0.60
	cfg.Storage.EmergencyRatio = 0.90
	cfg.Storage.MinFreeSpace = 0
	cfg.Storage.EmergencyReserve = 0
	cfg.Storage.CriticalReserve = 0
	path := writeRawTestSegment(t, cfg.Directory, 1, []Envelope{
		semanticTestEvent("flow-1", "flow.updated", "core-a", "flow-a", 1),
		semanticTestEvent("flow-2", "flow.updated", "core-a", "flow-a", 2),
		semanticTestEvent("flow-3", "flow.updated", "core-a", "flow-a", 3),
	})
	cfg.Storage.MaxSize = fileSize(t, path)

	spool := &FileSpool{
		cfg:           cfg,
		started:       true,
		ctx:           context.Background(),
		compressionCh: make(chan struct{}, 1),
	}
	spool.maintainStorage()

	if records, _, err := inspectRawSegment(path, fileSize(t, path), false); err != nil || records != 1 {
		t.Fatalf("pressure maintenance did not compact segment: records=%d err=%v", records, err)
	}
}
