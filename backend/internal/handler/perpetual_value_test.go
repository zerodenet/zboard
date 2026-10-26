package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Buy through the real assigned discount + funded-wallet path. A manual paid
// marker would not prove cash and must not make this refund test pass.
func perpetualValueFixtureWithBase(t *testing.T, f orderFixture, used int64) (orderFixture, model.Order) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 12000, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	actual := int64(10000)
	created, err := f.h.services.OrderAssignment.Assign(t.Context(), 1, commerce.OrderAssignmentRequest{UserID: 1, PlanSKUID: f.skuRecord.ID, PayableAmount: &actual, RequestID: uuid.NewString(), Note: "verified once purchase with real discount"})
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.Pay(t.Context(), 1, created.ID); err != nil {
		t.Fatal(err)
	}
	var root model.Order
	if err := f.h.db.First(&root, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	var fact commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(root.EntitlementValueSnapshot), &fact); err != nil || fact.AllocationModel != commerce.PerpetualQuotaAllocation || !fact.PaymentVerified || fact.ActualPaidMinor != actual || root.PaidAmount != actual || root.AmountCents != 12000 || root.DiscountAmount != 2000 || len(fact.PaidPeriods) != 0 {
		t.Fatalf("once purchase lost actual cash evidence: %+v %v", fact, err)
	}
	if used > 0 {
		var credential model.ProtocolCredential
		if err := f.h.db.Where("subscription_id = ? AND current_scope = ?", root.SubscriptionID, 1).First(&credential).Error; err != nil {
			t.Fatal(err)
		}
		accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.h.db, Cipher: f.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
		if _, _, err := accounting.Complete(t.Context(), metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "once-value-core", EventID: "once-value-use", EventType: "flow.completed", Sequence: 1, FlowID: "once-value-flow", PrincipalKey: credential.PrincipalKey, BytesDown: used, OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	return f, root
}

func TestPerpetualValuePurchaseMeteringAndIdempotentUnsubscribe(t *testing.T) {
	assertPerpetualValueUnsubscribe(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}

func TestMySQLPerpetualValuePurchaseMeteringAndIdempotentUnsubscribe(t *testing.T) {
	assertPerpetualValueUnsubscribe(t, newMySQLOrderFixture)
}

func assertPerpetualValueUnsubscribe(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, used := range []int64{0, 20, 100} {
		t.Run(fmt.Sprint(used), func(t *testing.T) {
			f, root := perpetualValueFixtureWithBase(t, fixture(t), used)
			value, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
			want := int64(10000) - used*100
			if err != nil || value.TotalMinor != want || value.FutureMinor != 0 {
				t.Fatalf("once quota residual %+v %v", value, err)
			}
			request := commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "once-unsubscribe"}
			var receiptID uint
			for range 2 {
				receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, request)
				if err != nil || receipt.AmountMinor != want || receiptID != 0 && receiptID != receipt.ID {
					t.Fatalf("duplicate once settlement %+v %v", receipt, err)
				}
				receiptID = receipt.ID
			}
			var wallet model.WalletAccount
			if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want || wallet.ReservedMinor != 0 {
				t.Fatalf("once unsubscribe created or lost cash %+v %v", wallet, err)
			}
			if err := f.h.db.First(&root, root.ID).Error; err != nil || root.RefundAmount != want || root.PaidAmount != 10000 || root.DiscountAmount != 2000 {
				t.Fatalf("once historical source budget changed %+v %v", root, err)
			}
			var settlements int64
			if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id = ?", root.SubscriptionID).Count(&settlements).Error; err != nil || settlements != 1 {
				t.Fatalf("once value settled more than once: %d %v", settlements, err)
			}
			if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "once-second-refund"}); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
				t.Fatalf("second key returned value again: %v", err)
			}
		})
	}
}

func TestPerpetualValueTrustedHistoricalSnapshotCompatibility(t *testing.T) {
	assertPerpetualValueHistoricalSnapshot(t, newOrderFixture(t))
}

func TestMySQLPerpetualValueTrustedHistoricalSnapshotCompatibility(t *testing.T) {
	assertPerpetualValueHistoricalSnapshot(t, newMySQLBusinessOrderFixture(t))
}

func assertPerpetualValueHistoricalSnapshot(t *testing.T, base orderFixture) {
	t.Helper()
	f, root := perpetualValueFixtureWithBase(t, base, 20)
	var fact commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(root.EntitlementValueSnapshot), &fact); err != nil {
		t.Fatal(err)
	}
	fact.AllocationModel = "value_requires_review" // Exact previous once capture format.
	payload, _ := json.Marshal(fact)
	if err := f.h.db.Model(&root).Update("entitlement_value_snapshot", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.h.db, Now: func() time.Time { return time.Date(2060, 1, 1, 0, 0, 0, 0, time.UTC) }}}
	value, err := values.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil || value.TotalMinor != 8000 {
		t.Fatalf("verified old once purchase unsupported %+v %v", value, err)
	}
	fact.PaymentVerified = false
	payload, _ = json.Marshal(fact)
	if err := f.h.db.Model(&root).Update("entitlement_value_snapshot", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := values.Read(t.Context(), 1, root.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("unverified old purchase became cash: %v", err)
	}
}

func TestPerpetualValueFullReplacementStartsCompleteNewTerm(t *testing.T) {
	for _, unit := range []string{"month", "once"} {
		t.Run(unit, func(t *testing.T) { assertPerpetualValueFullReplacement(t, newOrderFixture(t), unit) })
	}
}

func TestMySQLPerpetualValueFullReplacementStartsCompleteNewTerm(t *testing.T) {
	for _, unit := range []string{"month", "once"} {
		t.Run(unit, func(t *testing.T) { assertPerpetualValueFullReplacement(t, newMySQLBusinessOrderFixture(t), unit) })
	}
}

func assertPerpetualValueFullReplacement(t *testing.T, base orderFixture, unit string) {
	t.Helper()
	f, root := perpetualValueFixtureWithBase(t, base, 20)
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 2000, "change")
	if unit == "once" {
		if err := f.h.db.Model(&sku).Updates(map[string]any{"billing_unit": "once", "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	queries := entitlements.SubscriptionQueries{Repository: entitlementstore.SubscriptionQueries{DB: f.h.db}}
	page, err := queries.Owned(t.Context(), 1, entitlements.SubscriptionQuery{EligibleFor: "change", Limit: 10})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || !page.Items[0].CanChange {
		t.Fatalf("once source missing from real change entrance %+v %v", page, err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "once-full-replacement"}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil || quote.SettlementAmount != 8000 || quote.CurrentResidual != 8000 || quote.FutureResidual != 0 || quote.SurplusAmount != 6000 || quote.AdditionalAmount != 0 {
		t.Fatalf("wrong once replacement quote %+v %v", quote, err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	order, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	payments := fundingService(f)
	if _, err := payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	wantEnd, err := entitlements.AddBillingPeriod(sub.StartAt, unit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 100 || used != 0 || !sub.EndAt.Equal(wantEnd) || sub.EntitlementEpoch != 1 {
		t.Fatalf("once replacement carried old rights %+v", sub)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 26000 || wallet.ReservedMinor != 0 {
		t.Fatalf("once replacement did not conserve cash %+v %v", wallet, err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil || root.RefundAmount != 8000 || root.PaidAmount != 10000 || root.DiscountAmount != 2000 {
		t.Fatalf("once source was refunded twice or discount converted %+v %v", root, err)
	}
	value, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, sub.ID)
	if err != nil || value.TotalMinor != 2000 {
		t.Fatalf("new complete term lost paid value %+v %v", value, err)
	}
	requestEnd := commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "once-replacement-unsubscribe"}
	for range 2 {
		if receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, requestEnd); err != nil || receipt.AmountMinor != 2000 {
			t.Fatalf("new term idempotent refund %+v %v", receipt, err)
		}
	}
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 28000 || wallet.ReservedMinor != 0 {
		t.Fatalf("replacement and refund failed cash conservation %+v %v", wallet, err)
	}
}
