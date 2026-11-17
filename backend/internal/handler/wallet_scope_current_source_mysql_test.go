package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMySQLWalletPaymentRepreparesAddedSourceBeforeAnyDebit(t *testing.T) {
	f := newFullReplacementFixture(t, newMySQLBusinessOrderFixture(t), false, 0, 0)
	pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var before model.Subscription
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready, unblock, reads := pauseSettlementPreparation(t, f.db, ctx)
	type outcome struct {
		result commerce.WalletPaymentResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := f.payments.Pay(context.WithValue(ctx, settlementPreparationContextKey{}, true), 1, pending.ID)
		done <- outcome{result: result, err: err}
	}()
	joined := false
	defer func() {
		unblock()
		cancel()
		if !joined {
			<-done
		}
	}()
	select {
	case <-ready:
	case got := <-done:
		joined = true
		t.Fatal("wallet payment missed outside scope barrier", got.err)
	case <-ctx.Done():
		t.Fatal("wallet source preparation did not finish", ctx.Err())
	}
	// An ordinary supported writer adds a source-scope identity without changing
	// the paid term, accepted price, target quote or existing wallet allocation.
	competitor, err := f.orders.Create(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.subscription})
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	got := <-done
	joined = true
	if got.err != nil || !got.result.Fulfilled || reads.Load() < 2 {
		t.Fatal("wallet payment failed to rediscover pre-financial scope change", got.result, reads.Load(), got.err)
	}
	if replay, err := f.payments.Pay(ctx, 1, pending.ID); err != nil || replay.Fulfilled || replay.Status != "paid" {
		t.Fatal("wallet replay repeated debit or fulfillment", replay, err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.subscription).Error; err != nil || sub.EntitlementEpoch != before.EntitlementEpoch || !sub.StartAt.Equal(before.StartAt) || !sub.EndAt.Equal(before.EndAt.AddDate(1, 0, 0)) || sub.FlowTotal != before.FlowTotal || sub.FlowUsed != before.FlowUsed {
		t.Fatal("wallet scope reprepare changed more than one agreed extension", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 6000 || wallet.ReservedMinor != 0 {
		t.Fatal("wallet scope reprepare lost or duplicated reserved funds", wallet, err)
	}
	var other model.Order
	if err := f.db.First(&other, competitor.ID).Error; err != nil || other.Status != "pending" || other.PaidAmount != 0 || other.SubscriptionID != 0 {
		t.Fatal("wallet reprepare paid the concurrent pending intent", other, err)
	}
	var debits, audits int64
	if err := f.db.Model(&model.WalletEntry{}).Where("source_order_id = ? AND kind = ?", pending.ID, "purchase").Count(&debits).Error; err != nil || debits != 1 {
		t.Fatal("wallet debit identity duplicated", debits, err)
	}
	if err := f.db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "commerce.wallet.pay", fmt.Sprintf("order:%d", pending.ID)).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("wallet execution audit duplicated", audits, err)
	}
}
