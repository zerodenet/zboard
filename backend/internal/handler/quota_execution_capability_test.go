package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
	"testing"
	"time"
)

func TestQuotaExecutionFencesLeaseAndCommitsLedgerAtomically(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	until := time.Now().UTC().Add(time.Minute)
	task := model.Task{Type: "quota", Content: `{"delta_mb":1,"reason":"manual correction"}`, Scope: `{}`, Status: 1, LockedBy: "quota-owner", LockedUntil: &until, MaxAttempts: 3, IdempotencyKey: "quota-execution-fixture"}
	sub := model.Subscription{UserID: 1, FlowTotal: 10 * 1024 * 1024, FlowUsed: 2 * 1024 * 1024, EndAt: until, Status: "active"}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "subscription", TargetID: strconv.FormatUint(uint64(sub.ID), 10), Payload: `{}`, Status: 1}
	if err := h.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture publication failure")
	calls := 0
	store := entitlementstore.QuotaExecution{DB: h.db, Publish: func(tx *gorm.DB, id, actor uint) error { calls++; return failure }}
	service := entitlements.QuotaExecution{Repository: store}
	claim := entitlements.QuotaExecutionClaim{TaskID: task.ID, ItemID: item.ID, Token: "stale-owner"}
	if err := service.Execute(context.Background(), claim); !errors.Is(err, jobs.ErrLeaseLost) || calls != 0 {
		t.Fatal("stale lease executed", calls, err)
	}
	claim.Token = task.LockedBy
	if err := service.Execute(context.Background(), claim); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var current model.Subscription
	if err := h.db.First(&current, sub.ID).Error; err != nil || current.FlowTotal != sub.FlowTotal {
		t.Fatal("quota escaped rollback", current, err)
	}
	var count int64
	if err := h.db.Model(&model.QuotaEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("ledger escaped rollback", count, err)
	}
	store.Publish = func(tx *gorm.DB, id, actor uint) error { calls++; return nil }
	service.Repository = store
	for i := 0; i < 2; i++ {
		if err := service.Execute(context.Background(), claim); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("replay republished", calls)
	}
	if err := h.db.First(&current, sub.ID).Error; err != nil || current.FlowTotal != 11*1024*1024 {
		t.Fatal(current, err)
	}
	var events []model.QuotaEvent
	if err := h.db.Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if events[0].BalanceBefore != 8*1024*1024 || events[0].BalanceAfter != 9*1024*1024 {
		t.Fatal("incorrect ledger balances", events[0])
	}
	if err := h.db.Model(&task).Update("locked_by", "replacement-owner").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), claim); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("old lease replay accepted", err)
	}
}
