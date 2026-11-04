package observabilitystore

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCompletedFloorsExpireOnlyAfterTerminalNodeReportingFence(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "floor-retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(-1, 0, 0)
	for i := uint(1); i <= 3; i++ {
		node := model.Node{ID: i, Name: fmt.Sprint("node-", i), IsEnabled: false, LifecycleStatus: "archived", ArchivedAt: &old, NodeCredentialRevokedAt: &old, TrafficSecretRevokedAt: &old}
		if i == 1 {
			node.LifecycleStatus = "active"
		}
		if i == 2 {
			node.TrafficSecretRevokedAt = nil
		}
		if err := db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&node).Update("is_enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		for _, flow := range []string{"a", "b"} {
			if err := db.Create(&model.FlowCursorCheckpoint{NodeID: i, FlowID: flow, ProtocolCredentialID: 1, Revision: 9, UsedBytes: 40}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	store := HistoryRetention{DB: db}
	for i := 0; i < 2; i++ {
		if n, err := store.pruneArchivedFlowCursors(t.Context(), time.Now().UTC(), 1); err != nil || n != 1 {
			t.Fatal(n, err)
		}
	}
	if n, err := store.pruneArchivedFlowCursors(t.Context(), time.Now().UTC(), 1); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	var count int64
	if err := db.Model(&model.FlowCursorCheckpoint{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatal("live or partially fenced floors removed", count, err)
	}
}
