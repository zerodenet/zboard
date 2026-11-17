package jobstore

import (
	"context"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestBatchDeliveryCountsRemainTaskScopedAndAgreeAcrossListAndDetail(t *testing.T) {
	db := taskIdentityDB(t)
	admin := model.User{Email: "admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	tasks := []model.Task{
		{Type: "email", Scope: "{}", Content: "{}", IdempotencyKey: "delivery-one", MaxAttempts: 3},
		{Type: "email", Scope: "{}", Content: "{}", IdempotencyKey: "delivery-two", MaxAttempts: 3},
		{Type: "email", Scope: "{}", Content: "{}", IdempotencyKey: "delivery-empty", MaxAttempts: 3},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.TaskItem{
		{TaskID: tasks[0].ID, TargetType: "user", TargetID: "1", Payload: "{}", Status: 3, DeliveryState: "unknown"},
		{TaskID: tasks[0].ID, TargetType: "user", TargetID: "2", Payload: "{}", Status: 2, DeliveryState: "suppressed"},
		{TaskID: tasks[0].ID, TargetType: "user", TargetID: "3", Payload: "{}", Status: 2, DeliveryState: "accepted"},
		{TaskID: tasks[1].ID, TargetType: "user", TargetID: "4", Payload: "{}", Status: 1, DeliveryState: "unknown"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	repo := BatchQueries{DB: db}
	page, err := repo.List(context.Background(), admin.ID, jobs.BatchFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Items {
		detail, err := repo.Detail(context.Background(), admin.ID, row.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if row.NeedsReviewCount != detail.NeedsReviewCount || row.SuppressedCount != detail.SuppressedCount {
			t.Fatalf("list/detail differ: %+v / %+v", row, detail)
		}
		switch row.ID {
		case tasks[0].ID:
			if row.NeedsReviewCount != 1 || row.SuppressedCount != 1 || row.SucceededCount != 2 || row.FailedCount != 1 {
				t.Fatalf("first task counts: %+v", row)
			}
		case tasks[1].ID:
			if row.NeedsReviewCount != 1 || row.SuppressedCount != 0 {
				t.Fatalf("second task counts: %+v", row)
			}
		case tasks[2].ID:
			if row.NeedsReviewCount != 0 || row.SuppressedCount != 0 {
				t.Fatalf("empty task counts: %+v", row)
			}
		}
	}
	if len(page.Items) != 3 {
		t.Fatalf("tasks missing: %+v", page)
	}
}
