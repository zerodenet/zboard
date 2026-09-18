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

func TestOperationLogsOwnFilteringProjectionAndDetail(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	tasks := []model.Task{
		{Type: "email", Status: 2, Current: 2, Total: 2, Attempts: 1, MaxAttempts: 3, IdempotencyKey: "operation-log-1", CreatedAt: at.Add(-time.Minute)},
		{Type: "quota", Status: 3, Current: 1, Total: 2, Attempts: 2, MaxAttempts: 3, Errors: "failed", IdempotencyKey: "operation-log-2", CreatedAt: at},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	repository := OperationLogs{DB: db}
	page, err := repository.ListSource(context.Background(), observability.OperationLogQuery{
		Source: "task", From: at.Add(-time.Hour), To: at.Add(time.Hour), FetchLimit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].ID != tasks[1].ID || page.Items[0].Status != "failed" || !page.Items[0].HasError {
		t.Fatalf("page = %+v", page)
	}
	detail, err := repository.Detail(context.Background(), "task", tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Action != "task.email" || detail.Status != "succeeded" || detail.TargetID != tasks[0].ID {
		t.Fatalf("detail = %+v", detail)
	}
}
