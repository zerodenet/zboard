package handler

import (
	"context"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeJobsRequiresAdminAndReportsDurableQueue(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	rec := httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs", token, ""))
	if rec.Code != 403 {
		t.Fatalf("nonadmin: %d", rec.Code)
	}
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, _ = h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	now := time.Now().UTC()
	later := now.Add(time.Hour)
	for i, scheduled := range []*time.Time{nil, &now, &later} {
		row := model.Task{Type: "quota", Status: 0, ScheduledAt: scheduled, MaxAttempts: 3, IdempotencyKey: string(rune('a' + i))}
		if err := h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	rec = httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs", token, ""))
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Queues []runtimeQueue `json:"queues"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	q := body.Data.Queues[1]
	if q.ID != "admin_tasks" || q.Pending != 1 || q.Delayed != 1 || q.Drafts != 1 {
		t.Fatalf("queue %+v", q)
	}
	rec = httptest.NewRecorder()
	h.AdminRuntimeQueueHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs/queue?name=admin_tasks&limit=1", token, ""))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var page struct {
		Data struct {
			Items []runtimeQueueItem
			Total int64
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Data.Items) != 1 || page.Data.Total != 3 {
		t.Fatalf("pagination %s", rec.Body.String())
	}
}
func TestExpiredTaskOwnerCannotWriteItemResult(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	old := time.Now().UTC().Add(-time.Minute)
	task := model.Task{Type: "quota", Status: taskStatusRunning, LockedBy: "old", LockedUntil: &old, IdempotencyKey: "lease", MaxAttempts: 3}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	err := h.withTaskLease(task.ID, "old", func(tx *gorm.DB) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("expired owner was accepted")
	}
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	h.db.Model(&task).Updates(map[string]any{"locked_by": "new", "locked_until": future})
	err = h.withTaskLease(task.ID, "old", func(tx *gorm.DB) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("replaced owner was accepted")
	}
}
func TestQueueRecoveryRespectsDraftsAndRequiresExplicitInterruptedRetry(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	past := time.Now().UTC().Add(-time.Hour)
	draft := model.Task{Type: "quota", MaxAttempts: 3, IdempotencyKey: "draft"}
	queued := model.Task{Type: "unsupported", ScheduledAt: &past, MaxAttempts: 3, IdempotencyKey: "queued"}
	stale := model.Task{Type: "quota", Status: 1, LockedUntil: &past, LockedBy: "old", MaxAttempts: 3, IdempotencyKey: "stale"}
	for _, row := range []*model.Task{&draft, &queued, &stale} {
		if err := h.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	h.StartAdminTaskWorker()
	defer h.CloseBackgroundJobs()
	deadline := time.Now().UTC().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.db.First(&queued, queued.ID)
		if queued.Status == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.db.First(&draft, draft.ID)
	h.db.First(&stale, stale.ID)
	if queued.Status != 2 || draft.Status != 0 || draft.Attempts != 0 || stale.Status != 3 || stale.Attempts != 0 {
		t.Fatalf("recovery queued=%+v draft=%+v stale=%+v", queued, draft, stale)
	}
}
func TestScheduledJobDoesNotOverlapAndStops(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	var active, max atomic.Int32
	entered := make(chan struct{}, 1)
	h.startScheduledJob("test", time.Millisecond, func(ctx context.Context) error {
		n := active.Add(1)
		if n > max.Load() {
			max.Store(n)
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		active.Add(-1)
		return ctx.Err()
	})
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("did not start")
	}
	h.closeScheduledJob("test")
	if active.Load() != 0 || max.Load() != 1 || h.backgroundJobs().observations.Snapshot()[0].State != "stopped" {
		t.Fatal("scheduler failed to stop")
	}
}

func TestHistoryRetentionBatchesAndPreservesActiveTasks(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	old := time.Now().UTC().AddDate(-1, 0, 0)
	rows := make([]model.AuditLog, 1001)
	for i := range rows {
		rows[i] = model.AuditLog{Actor: "test", Action: "test", CreatedAt: old}
	}
	if err := h.db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "quota", Status: taskStatusPending, FinishedAt: &old, IdempotencyKey: "pending-retry", MaxAttempts: 3}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	result, err := h.runHistoryRetention(time.Now().UTC())
	if err != nil || result.AuditLogs != 1001 {
		t.Fatal(result, err)
	}
	var count int64
	h.db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&count)
	if count != 1 {
		t.Fatal("cleanup removed queued retry")
	}
}

func TestRuntimeQueueDatabaseFailureIsNotAnEmptyQueue(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true)
	token, _, _ := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err := h.db.Exec("ALTER TABLE tasks RENAME TO hidden_tasks").Error; err != nil {
		t.Fatal(err)
	}
	defer h.db.Exec("ALTER TABLE hidden_tasks RENAME TO tasks")
	rec := httptest.NewRecorder()
	h.AdminRuntimeJobsHandler(rec, announcementRequest("GET", "/api/v1/admin/runtime-jobs", token, ""))
	if rec.Code != 503 {
		t.Fatal("database failure was not surfaced", rec.Code, rec.Body.String())
	}
}
