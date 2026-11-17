package jobstore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestBatchExecutionLoadDoesNotSelectAdministrativeDisplayColumns(t *testing.T) {
	db := taskIdentityDB(t)
	until := time.Now().UTC().Add(time.Minute)
	task := model.Task{Type: "quota", Scope: "{}", Content: "{}", Status: 1, LockedBy: "execution-lease", LockedUntil: &until, IdempotencyKey: "execution-read-model", MaxAttempts: 3}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: "4", Payload: "{}"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := (BatchRun{DB: db}).Load(context.Background(), task.ID, task.LockedBy)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Items) != 1 || loaded.Items[0].ID != item.ID || loaded.Items[0].TargetID != "4" {
		t.Fatalf("execution item changed: %+v", loaded)
	}
}
