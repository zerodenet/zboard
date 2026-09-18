package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type taskMailFunc func(context.Context, messaging.Message) error

func (f taskMailFunc) SendTaskMessage(ctx context.Context, message messaging.Message) error {
	return f(ctx, message)
}
func TestEmailExecutionUsesCurrentRecipientAndFencesStaleCompletion(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	var user model.User
	if err := h.db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	user.AccountName = "Current name"
	if err := h.db.Save(&user).Error; err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(messaging.EmailContent{Subject: "Hello {{account_name}}", Body: "{{user_email}} / {{site_name}}", SiteName: "Snapshot"})
	until := time.Now().Add(time.Minute)
	task := model.Task{Type: "email", Status: 1, Content: string(content), Scope: `{}`, LockedBy: "lease", LockedUntil: &until, IdempotencyKey: "mail-execution-fixture"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := newTaskItem("user", user.ID)
	item.TaskID = task.ID
	item.Status = 1
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	service := messaging.EmailExecution{Repository: messagingstore.EmailExecution{DB: h.db}, Sender: taskMailFunc(func(ctx context.Context, message messaging.Message) error {
		calls++
		if message.Recipient != user.Email || message.Subject != "Hello Current name" || message.Body != user.Email+" / Snapshot" {
			t.Fatal("incorrect persisted recipient/content", message)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded mail call")
		}
		// This write also proves the preparation transaction ended before send.
		return h.db.Model(&task).Update("locked_by", "replacement").Error
	})}
	claim := messaging.EmailExecutionClaim{TaskID: task.ID, ItemID: item.ID, Token: "stale"}
	if err := service.Execute(context.Background(), claim); !errors.Is(err, jobs.ErrLeaseLost) || calls != 0 {
		t.Fatal("stale claim sent mail", calls, err)
	}
	claim.Token = "lease"
	if err := service.Execute(context.Background(), claim); !errors.Is(err, jobs.ErrLeaseLost) || messaging.AcceptanceFor(err) != messaging.AcceptanceUnknown || calls != 1 {
		t.Fatal("stale completion accepted", calls, err)
	}
	claim.Token = "replacement"
	if err := h.db.Model(&user).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), claim); err == nil || calls != 1 {
		t.Fatal("inactive recipient sent mail", calls, err)
	}
}
