package datastore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCompletedFlowPreparationPreservesCollationAndFailsOnLostAuthority(t *testing.T) {
	db, err := OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "checkpoints.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE flow_usages (id INTEGER PRIMARY KEY, node_id INTEGER NOT NULL, flow_id TEXT COLLATE NOCASE NOT NULL, UNIQUE(node_id,flow_id))").Error; err != nil {
		t.Fatal(err)
	}
	// Resume after the first additive CREATE committed, without replacing rows.
	if err := db.Exec("CREATE TABLE flow_cursor_checkpoints (node_id INTEGER NOT NULL, flow_id TEXT COLLATE NOCASE NOT NULL, protocol_credential_id INTEGER NOT NULL, revision INTEGER NOT NULL, raw_bytes INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, used_bytes INTEGER NOT NULL, is_completed NUMERIC NOT NULL, PRIMARY KEY(node_id,flow_id))").Error; err != nil {
		t.Fatal(err)
	}
	row := model.FlowCursorCheckpoint{IsCompleted: true, NodeID: 1, FlowID: "Original", Revision: 9, RawBytes: 42}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reconcileFlowCursorCheckpointSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	var got model.FlowCursorCheckpoint
	if err := db.Where("node_id=1 AND flow_id=?", "ORIGINAL").First(&got).Error; err != nil || got.RawBytes != 42 {
		t.Fatal(got, err)
	}
	if err := db.Migrator().DropTable(&model.FlowCursorCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db); err == nil || !strings.Contains(err.Error(), "authority shape") {
		t.Fatal("missing recorded authority was recreated", err)
	}
	if db.Migrator().HasTable(&model.FlowCursorCheckpoint{}) {
		t.Fatal("lost authority recreated as empty")
	}
}

func TestCompletedFlowCopyIncludesPermanentFloorAndScanCheckpoint(t *testing.T) {
	open := func(name string) *gorm.DB {
		db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := db.DB()
		t.Cleanup(func() { pool.Close() })
		if err := RunMigrations(db); err != nil {
			t.Fatal(err)
		}
		return db
	}
	source := open("source.db")
	target := open("target.db")
	row := model.FlowCursorCheckpoint{IsCompleted: true, NodeID: 4, FlowID: "old-runtime/flow", ProtocolCredentialID: 9, Revision: 30, RawBytes: 140, UploadBytes: 40, DownloadBytes: 100, UsedBytes: 280}
	if err := source.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	state := model.FlowCursorCompactionState{ID: 1, AfterID: 55, NextPassAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := source.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	tables, err := MigrationTables(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Transaction(func(tx *gorm.DB) error {
		if err := ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal(err)
	}
	var got model.FlowCursorCheckpoint
	if err := target.First(&got).Error; err != nil || got != row {
		t.Fatal(got, err)
	}
	var progress model.FlowCursorCompactionState
	if err := target.First(&progress).Error; err != nil || progress.AfterID != 55 || !progress.NextPassAt.Equal(state.NextPassAt) {
		t.Fatal(progress, err)
	}
}
