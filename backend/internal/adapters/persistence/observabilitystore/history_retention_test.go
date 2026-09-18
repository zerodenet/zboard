package observabilitystore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestHistoryRetentionPrunesOnlyTerminalBoundedHistory(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	service := observability.HistoryRetention{Repository: HistoryRetention{DB: db}}
	if err := service.ReconcileDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(-1, 0, 0)
	finished := model.Task{Type: "email", Scope: `{}`, Content: `{}`, Status: 2, FinishedAt: &old, IdempotencyKey: "finished"}
	running := model.Task{Type: "email", Scope: `{}`, Content: `{}`, Status: 1, FinishedAt: &old, IdempotencyKey: "running"}
	if err := db.Create(&finished).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&running).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: finished.ID, TargetType: "user", TargetID: "1", Payload: `{}`, Status: 2}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	attempt := model.MailDeliveryAttempt{TaskID: finished.ID, ItemID: item.ID, Attempt: 1, LeaseToken: "lease", Acceptance: "accepted", StartedAt: old}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuditLog{Actor: "test", Action: "old", Target: "old", CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.Run(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.Tasks != 1 || result.AuditLogs != 1 {
		t.Fatalf("retention result = %+v", result)
	}
	for name, value := range map[string]any{"task": &model.Task{}, "item": &model.TaskItem{}, "attempt": &model.MailDeliveryAttempt{}} {
		var count int64
		query := db.Model(value)
		if name == "task" {
			query = query.Where("id = ?", finished.ID)
		} else {
			query = query.Where("task_id = ?", finished.ID)
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s retained count=%d err=%v", name, count, err)
		}
	}
	var runningCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", running.ID).Count(&runningCount).Error; err != nil || runningCount != 1 {
		t.Fatalf("running task count=%d err=%v", runningCount, err)
	}
}
