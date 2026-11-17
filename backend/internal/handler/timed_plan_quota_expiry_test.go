package handler

import (
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTimedPlanSyncExpiryReadsZeroAndCancelsWithoutCredit(t *testing.T) {
	assertTimedPlanSyncExpiryRead(t, newOrderFixture(t))
}
func TestMySQLTimedPlanSyncExpiryReadsZeroAndCancelsWithoutCredit(t *testing.T) {
	assertTimedPlanSyncExpiryRead(t, newMySQLBusinessOrderFixture(t))
}
func assertTimedPlanSyncExpiryRead(t *testing.T, base orderFixture) {
	f, _ := timedPlanSyncFixture(t, base)
	updates, values := timedPlanSyncServices(f)
	var original model.Subscription
	var plan model.Plan
	if err := f.db.First(&original, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, original.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	quota := int64(200)
	if _, err := updates.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	// A second independent real paid root starts AFTER the sync and therefore
	// has no timed-cap history. Both paths share one isolated schema, not fake
	// terminal markers or rewritten historical payment snapshots.
	made, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "timed-sync-ordinary-expiry-root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, made.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, made.ID); err != nil {
		t.Fatal(err)
	}
	var ordinary model.Order
	if err := f.db.First(&ordinary, made.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ordinary.SubscriptionID == f.subscription || ordinary.SubscriptionID == 0 {
		t.Fatal("new paid root reused another independent subscription", ordinary)
	}
	var second model.Subscription
	if err := f.db.First(&second, ordinary.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	*f.at = second.EndAt.Add(time.Hour)
	var before model.WalletAccount
	if err := f.db.Where("user_id=? AND currency=?", 1, "CNY").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	// Service time has ended even if the lifecycle worker has not run yet.
	// Reading zero may not rewrite markers or depend on a catch-up reset.
	for _, id := range []uint{f.subscription, ordinary.SubscriptionID} {
		var pending model.Subscription
		if err := f.db.First(&pending, id).Error; err != nil {
			t.Fatal(err)
		}
		if pending.EndedAt != nil || pending.Status != "active" {
			t.Fatal("fixture expiry worker already ran", pending)
		}
		quote, err := values.Read(t.Context(), 1, id)
		if err != nil || quote.TotalMinor != 0 || quote.CurrentMinor != 0 || quote.FutureMinor != 0 || quote.Fingerprint == "" {
			t.Fatal("unprocessed natural expiry lost its zero quote", quote, err)
		}
		var unchanged model.Subscription
		if err := f.db.First(&unchanged, id).Error; err != nil {
			t.Fatal(err)
		}
		if unchanged.EndedAt != nil || unchanged.Status != pending.Status || unchanged.FlowTotal != pending.FlowTotal || unchanged.FlowUsed != pending.FlowUsed || unchanged.CycleStartUsed != pending.CycleStartUsed {
			t.Fatal("lazy expiry zero read changed rights", unchanged)
		}
	}
	if _, err := base.h.services.CredentialExpiry(base.h.credentialCipher).ExpireDue(t.Context(), *f.at, 20); err != nil {
		t.Fatal(err)
	}
	terminal := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: func() time.Time { return *f.at }}}
	for _, id := range []uint{f.subscription, ordinary.SubscriptionID} {
		var ended model.Subscription
		if err := f.db.First(&ended, id).Error; err != nil {
			t.Fatal(err)
		}
		if ended.Status != "expired" || ended.EndReason != "expired" || ended.EndedAt == nil || !ended.EndedAt.Equal(ended.EndAt) || ended.Lifecycle != "renewable" {
			t.Fatal("real expiry fixture did not enter its recovery window", ended)
		}
		quote, err := values.Read(t.Context(), 1, id)
		if err != nil || quote.TotalMinor != 0 || quote.CurrentMinor != 0 || quote.FutureMinor != 0 || quote.Fingerprint == "" {
			t.Fatal("canonical expired subscription lost its existing zero quote", quote, err)
		}
		var still model.Subscription
		if err := f.db.First(&still, id).Error; err != nil {
			t.Fatal(err)
		}
		if still.Status != "expired" || still.EndedAt == nil || !still.EndedAt.Equal(*ended.EndedAt) {
			t.Fatal("zero read revived terminal rights", still)
		}
		request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: fmt.Sprintf("timed-sync-expired-zero-%d", id)}
		for range 2 {
			receipt, err := terminal.Terminate(t.Context(), 1, id, request)
			if err != nil || receipt.AmountMinor != 0 {
				t.Fatal("zero whole cancellation failed or created money", receipt, err)
			}
		}
		if err := f.db.First(&still, id).Error; err != nil || still.Status != "canceled" || still.EndedAt == nil {
			t.Fatal("expired whole cancellation did not revoke recovery rights", still, err)
		}
	}
	var after model.WalletAccount
	if err := f.db.Where("user_id=? AND currency=?", 1, "CNY").First(&after).Error; err != nil || after.BalanceMinor != before.BalanceMinor || after.ReservedMinor != before.ReservedMinor {
		t.Fatal("expiry/read/zero replay changed money", after, err)
	}
}
