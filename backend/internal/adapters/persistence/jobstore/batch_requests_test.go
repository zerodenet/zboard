package jobstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestBatchRequestsRecheckAdministratorAndCommitTaskItemsAuditAtomically(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "batch-requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "batch-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	store := BatchRequests{DB: db}
	submission := jobs.BatchSubmission{Type: "node_lifecycle", Scope: `{"node_ids":[1,2]}`, Content: `{"action":"enable"}`, IdempotencyKey: "batch-request", MaxAttempts: 3, ScheduledAt: time.Now().UTC(), Targets: []jobs.BatchSubmissionTarget{{Type: "node", ID: 1}, {Type: "node", ID: 2}}}
	receipt, err := store.SubmitBatchRequest(context.Background(), admin.ID, submission)
	if err != nil || receipt.ID == 0 || receipt.Total != 2 {
		t.Fatalf("receipt = %+v err=%v", receipt, err)
	}
	var items, audits int64
	if err := db.Model(&model.TaskItem{}).Where("task_id = ?", receipt.ID).Count(&items).Error; err != nil || items != 2 {
		t.Fatalf("items=%d err=%v", items, err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "task.create", "task:1").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	submission.IdempotencyKey = "revoked"
	if _, err := store.SubmitBatchRequest(context.Background(), admin.ID, submission); !errors.Is(err, jobs.ErrBatchRequestPermission) {
		t.Fatalf("revoked admin error = %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_batch_request_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'task.create' BEGIN SELECT RAISE(ABORT, 'audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	submission.IdempotencyKey = "audit-failure"
	if _, err := store.SubmitBatchRequest(context.Background(), admin.ID, submission); err == nil {
		t.Fatal("audit failure accepted")
	}
	var tasks int64
	if err := db.Model(&model.Task{}).Where("idempotency_key = ?", "audit-failure").Count(&tasks).Error; err != nil || tasks != 0 {
		t.Fatalf("rolled-back tasks=%d err=%v", tasks, err)
	}
}
