package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type legacyZeroFixture struct {
	base       orderFixture
	db         *gorm.DB
	sub        model.Subscription
	root, free model.Order
	target     uint
	now        time.Time
}

func newLegacyZeroFixture(t *testing.T, mysql, mixed, activePaid, perpetual bool) legacyZeroFixture {
	t.Helper()
	var b orderFixture
	if mysql {
		b = newMySQLBusinessOrderFixture(t)
	} else {
		b = newOrderFixture(t)
	}
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	db := b.h.db.Session(&gorm.Session{NowFunc: func() time.Time { return now }})
	start := time.Date(2026, 9, 6, 17, 16, 5, 785000000, time.UTC)
	end, _ := entitlements.AddBillingPeriod(start, "month", 1)
	if !mixed {
		end, _ = entitlements.AddBillingPeriod(start, "year", 1)
	}
	if activePaid {
		end, _ = entitlements.AddBillingPeriod(start, "year", 1)
	}
	oldPlan := b.planRecord
	if err := db.Model(&oldPlan).Update("traffic_bytes", 100).Error; err != nil {
		t.Fatal(err)
	}
	currentPlan := oldPlan
	currentSKU := b.skuRecord
	if mixed {
		currentPlan = b.plan(t, 2)
		currentSKU = b.sku(t, currentPlan.ID, 0, "purchase")
	}
	finalEnd := end
	if mixed {
		finalEnd, _ = entitlements.AddBillingPeriod(end, "year", 1)
	}
	if perpetual {
		finalEnd = entitlements.PerpetualEnd
	}
	total := int64(100)
	if mixed {
		total += 5000
	}
	sub := model.Subscription{UserID: 1, PlanID: currentPlan.ID, PlanSKUID: currentSKU.ID, NodeGroupID: currentPlan.NodeGroupID, StartAt: start, EndAt: finalEnd, Status: "active", FlowTotal: total, FlowUsed: 40, ResetQuotaBytes: total, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	price := int64(0)
	unit := "month"
	if !mixed {
		unit = "year"
	}
	if mixed {
		price = 1500
	}
	if activePaid {
		unit = "year"
	}
	if perpetual {
		unit = "once"
	}
	root := model.Order{UserID: 1, SubscriptionID: sub.ID, PlanID: oldPlan.ID, PlanSKUID: b.skuRecord.ID, OrderType: "new", Status: "paid", TradeNo: "legacy-root", Currency: "CNY", AmountCents: price, PayableAmount: price, PaidAmount: price, BillingUnit: unit, BillingValue: 1, RenewalEffect: "none", TrafficBytes: 100, PaidAt: &start, FulfilledAt: &start}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EventType: "purchase", DeltaBytes: 100, BalanceBefore: 0, BalanceAfter: 100, ReferenceType: "order", ReferenceID: fmt.Sprint(root.ID), Detail: "{}", CreatedAt: start.Add(6 * time.Millisecond)}).Error; err != nil {
		t.Fatal(err)
	}
	var free model.Order
	if mixed {
		grant := start.AddDate(0, 0, 5)
		free = model.Order{UserID: 1, SubscriptionID: sub.ID, PlanID: currentPlan.ID, PlanSKUID: currentSKU.ID, OrderType: "upgrade", Status: "paid", TradeNo: "legacy-free", Currency: "CNY", BillingUnit: "year", BillingValue: 1, RenewalEffect: "extend_only", TrafficBytes: 5000, PaidAt: &grant, FulfilledAt: &grant}
		if err := db.Create(&free).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EventType: "upgrade", DeltaBytes: 5000, BalanceBefore: 80, BalanceAfter: 5080, ReferenceType: "order", ReferenceID: fmt.Sprint(free.ID), Detail: "{}", CreatedAt: grant.Add(13 * time.Millisecond)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	targetPlan := b.plan(t, 3)
	if err := db.Model(&targetPlan).Update("traffic_bytes", 300).Error; err != nil {
		t.Fatal(err)
	}
	target := b.sku(t, targetPlan.ID, 22000, "change")
	return legacyZeroFixture{b, db, sub, root, free, target.ID, now}
}
func (f legacyZeroFixture) preview(t *testing.T) (commerce.OrderPreview, error) {
	return (commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: func() time.Time { return f.now }}}).Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.sub.ID})
}
func TestLegacyZeroValueMigrationAndQuote(t *testing.T)      { assertLegacyZeroQuote(t, false) }
func TestMySQLLegacyZeroValueMigrationAndQuote(t *testing.T) { assertLegacyZeroQuote(t, true) }
func assertLegacyZeroQuote(t *testing.T, mysql bool) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			f := newLegacyZeroFixture(t, mysql, mixed, false, false)
			var before model.Subscription
			f.db.First(&before, f.sub.ID)
			for i := 0; i < 2; i++ {
				q, e := f.preview(t)
				if e != nil || q.SettlementAmount != 0 || q.AdditionalAmount != 22000 || q.CreditAmount != 0 || q.QuoteFingerprint == "" {
					t.Fatal(q, e)
				}
			}
			var sub model.Subscription
			f.db.First(&sub, f.sub.ID)
			if !reflect.DeepEqual(before, sub) {
				t.Fatal("migration changed entitlement", before, sub)
			}
			var events []model.QuotaEvent
			f.db.Where("subscription_id=? AND event_type='legacy_value_baseline'", sub.ID).Find(&events)
			if len(events) != 1 || events[0].DeltaBytes != 0 || events[0].BalanceBefore != events[0].BalanceAfter {
				t.Fatal(events)
			}
			var detail map[string]any
			if json.Unmarshal([]byte(events[0].Detail), &detail) != nil || detail["kind"] != "legacy_zero_payment_migration" || detail["source_sha256"] == "" {
				t.Fatal(detail)
			}
			var original model.Order
			f.db.First(&original, f.root.ID)
			if original.EntitlementValueSnapshot != "" || original.PurchaseSnapshot != "" || original.PaidAmount != f.root.PaidAmount {
				t.Fatal("rewrote historical order", original)
			}
			// Current catalog repricing must not manufacture money or alter instance rights.
			f.db.Model(&model.PlanSKU{}).Where("id=?", sub.PlanSKUID).Update("price_cents", 99999)
			q, e := f.preview(t)
			if e != nil || q.CreditAmount != 0 {
				t.Fatal(q, e)
			}
		})
	}
}
func TestLegacyZeroDoesNotErasePaidOrTransferredValue(t *testing.T) {
	for _, kind := range []string{"active_paid", "transfer", "receipt", "funding", "bad_quota", "source_changed", "corrupt_baseline", "oversize_baseline", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			f := newLegacyZeroFixture(t, false, kind == "active_paid" || kind == "bad_quota", kind == "active_paid", false)
			switch kind {
			case "transfer":
				f.db.Model(&f.root).Update("change_snapshot", `{"credit":500}`)
			case "receipt":
				f.db.Create(&model.PaymentReceipt{OrderID: f.root.ID, ReceiptKey: "unexpected", Provider: "test", ProviderTradeNo: "unexpected", AmountMinor: 10, Currency: "CNY", Disposition: "held"})
			case "funding":
				f.db.Create(&model.OrderFunding{OrderID: f.root.ID, UserID: 1, Currency: "CNY", AmountMinor: 10, WalletMinor: 10, State: "captured", CapturedAt: &f.now, ExpiresAt: f.now})
			case "bad_quota":
				f.db.Model(&model.QuotaEvent{}).Where("reference_id=? AND event_type='upgrade'", fmt.Sprint(f.free.ID)).Update("balance_after", 999)
			case "source_changed":
				if _, e := f.preview(t); e != nil {
					t.Fatal(e)
				}
				f.db.Model(&f.root).Update("paid_amount", 10)
			case "corrupt_baseline", "oversize_baseline":
				if _, e := f.preview(t); e != nil {
					t.Fatal(e)
				}
				detail := "{}"
				if kind == "oversize_baseline" {
					detail = `{"padding":"` + strings.Repeat("x", 65536) + `"}`
				}
				if e := f.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type='legacy_value_baseline'", f.sub.ID).Update("detail", detail).Error; e != nil {
					t.Fatal(e)
				}
			case "rollback":
				f.db.Callback().Create().Before("gorm:create").Register("test:baseline_failure", func(tx *gorm.DB) {
					if e, ok := tx.Statement.Dest.(*model.QuotaEvent); ok && e.EventType == "legacy_value_baseline" {
						tx.AddError(errors.New("injected migration write failure"))
					}
				})
				defer f.db.Callback().Create().Remove("test:baseline_failure")
			}
			if q, e := f.preview(t); e == nil {
				t.Fatal("unproven value accepted", q)
			}
			var count int64
			f.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type='legacy_value_baseline'", f.sub.ID).Count(&count)
			if kind != "source_changed" && kind != "corrupt_baseline" && kind != "oversize_baseline" && count != 0 {
				t.Fatal("failed migration persisted", count)
			}
		})
	}
}

func payLegacyMigrationOrder(t *testing.T, f legacyZeroFixture, order commerce.Order) {
	t.Helper()
	issuer := zeroadapter.Issuer{Cipher: f.base.h.credentialCipher, Mieru: f.base.h.zeroMieruAccess}
	clock := func() time.Time { return f.now }
	payments := commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: f.db, Issuer: issuer, Now: clock}}
	if _, e := payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: false}); e != nil {
		t.Fatal(e)
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: f.db, Issuer: issuer, Now: clock}}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: fmt.Sprint("legacy-migration-payment-", order.ID), ProviderTradeNo: fmt.Sprint("legacy-migration-trade-", order.ID), Status: "paid", AmountMinor: order.PayableAmount, Currency: "CNY", OccurredAt: f.now}
	for i := 0; i < 2; i++ {
		if _, e := facts.Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "legacy-migration-test", RequireAdministrator: true}, fact); e != nil {
			t.Fatal(e)
		}
	}
	var paid model.Order
	if err := f.db.First(&paid, order.ID).Error; err != nil || paid.Status != "paid" || paid.FulfilledAt == nil {
		t.Fatal("payment was held or not fulfilled", paid.Status, paid.FulfilledAt, err)
	}
}
func TestLegacyZeroReplacementCompletesWithoutInventingRefund(t *testing.T) {
	assertLegacyZeroReplacement(t, false)
}
func TestMySQLLegacyZeroReplacementCompletesWithoutInventingRefund(t *testing.T) {
	assertLegacyZeroReplacement(t, true)
}
func assertLegacyZeroReplacement(t *testing.T, mysql bool) {
	f := newLegacyZeroFixture(t, mysql, true, false, false)
	attachOrderPublishEndpoint(t, f.base)
	q, e := f.preview(t)
	if e != nil {
		t.Fatal(e)
	}
	orders := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: func() time.Time { return f.now }}}
	order, e := orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.sub.ID, QuoteFingerprint: q.QuoteFingerprint, IdempotencyKey: "legacy-replace"})
	if e != nil {
		t.Fatal(e)
	}
	payLegacyMigrationOrder(t, f, order)
	var after model.Subscription
	if e := f.db.First(&after, f.sub.ID).Error; e != nil {
		t.Fatal(e)
	}
	if after.PlanSKUID != f.target || after.FlowTotal-after.CycleStartUsed != order.TrafficBytes || after.FlowUsed != f.sub.FlowUsed || after.CycleStartUsed != after.FlowUsed || !after.StartAt.Equal(f.now) {
		t.Fatal("replacement stacked old rights", after, q)
	}
	for _, id := range []uint{f.root.ID, f.free.ID} {
		var old model.Order
		f.db.First(&old, id)
		if old.RefundAmount != 0 || old.SubscriptionEndedAt == nil || old.EntitlementValueSnapshot != "" {
			t.Fatal("old financial history changed", old)
		}
	}
	var settlements []model.SubscriptionValueSettlement
	f.db.Where("subscription_id=?", f.sub.ID).Find(&settlements)
	if len(settlements) != 1 || settlements[0].AmountMinor != 0 || settlements[0].WalletMinor != 0 || settlements[0].AppliedMinor != 0 {
		t.Fatal(settlements)
	}
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return f.now }}}
	value, e := values.Read(t.Context(), 1, f.sub.ID)
	if e != nil || value.TotalMinor != order.PayableAmount {
		t.Fatal("new paid value lost", value, e)
	}
}
func TestLegacyZeroMigrationPreservesLaterPaidRenewal(t *testing.T) {
	assertLegacyZeroLaterPaidRenewal(t, false)
}
func TestMySQLLegacyZeroMigrationPreservesLaterPaidRenewal(t *testing.T) {
	assertLegacyZeroLaterPaidRenewal(t, true)
}
func assertLegacyZeroLaterPaidRenewal(t *testing.T, mysql bool) {
	f := newLegacyZeroFixture(t, mysql, false, false, false)
	attachOrderPublishEndpoint(t, f.base)
	// A pending order is not a paid source at migration capture time.
	// Its later quota event identifies the new paid contribution.
	sku := f.base.sku(t, f.sub.PlanID, 1200, "renew")
	orders := commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: func() time.Time { return f.now }}}
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: f.sub.ID, IdempotencyKey: "legacy-paid-renewal"}
	quote, e := orders.Preview(t.Context(), 1, request)
	if e != nil {
		t.Fatal(e)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	pending, e := orders.Create(t.Context(), 1, request)
	if e != nil {
		t.Fatal(e)
	}
	// Imported historical zero-price grant metadata has a later ID than the
	// pending order. It does not change rights or invalidate that purchase quote.
	extra := f.root
	extra.ID, extra.TradeNo, extra.OrderType = 0, "legacy-free-after-pending", "traffic_pack"
	grantAt := f.root.PaidAt.AddDate(0, 0, 10)
	extra.PaidAt, extra.FulfilledAt = &grantAt, &grantAt
	extra.TrafficBytes = 0
	if e := f.db.Create(&extra).Error; e != nil {
		t.Fatal(e)
	}
	if extra.ID <= pending.ID {
		t.Fatal("fixture lacks a pending order ID hole")
	}
	if e := f.db.Create(&model.QuotaEvent{SubscriptionID: f.sub.ID, EventType: "traffic_pack", ReferenceType: "order", ReferenceID: fmt.Sprint(extra.ID), BalanceBefore: 60, BalanceAfter: 60, Detail: "{}", CreatedAt: grantAt}).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := f.preview(t); e != nil {
		t.Fatal(e)
	}
	payLegacyMigrationOrder(t, f, pending)
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return f.now }}}
	value, e := values.Read(t.Context(), 1, f.sub.ID)
	if e != nil || value.TotalMinor != 1200 {
		t.Fatal("paid renewal erased by zero baseline", value, e)
	}
}
func TestLegacyZeroPerpetualMigrationKeepsConsumption(t *testing.T) {
	f := newLegacyZeroFixture(t, false, false, false, true)
	q, e := f.preview(t)
	if e != nil || q.CreditAmount != 0 || q.AdditionalAmount != 22000 {
		t.Fatal(q, e)
	}
	values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return f.now }}}
	value, e := values.Read(t.Context(), 1, f.sub.ID)
	if e != nil || value.TotalMinor != 0 {
		t.Fatal(value, e)
	}
	var after model.Subscription
	f.db.First(&after, f.sub.ID)
	if after.FlowUsed != 40 || after.FlowTotal != 100 || after.CycleStartUsed != 0 {
		t.Fatal(after)
	}
}

func TestMySQLLegacyZeroConcurrentPreviewCreatesOneBaseline(t *testing.T) {
	f := newLegacyZeroFixture(t, true, false, false, false)
	type result struct {
		quote commerce.OrderPreview
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; q, err := f.preview(t); results <- result{q, err} }()
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			success++
			if r.quote.CreditAmount != 0 || r.quote.AdditionalAmount != 22000 {
				t.Fatal(r.quote)
			}
		} else {
			var conflict *commerce.ValidationError
			if !errors.As(r.err, &conflict) {
				t.Fatal("unclassified migration race", r.err)
			}
		}
	}
	if success == 0 {
		t.Fatal("no preview completed")
	}
	if _, err := f.preview(t); err != nil {
		t.Fatal("retry did not converge", err)
	}
	var count int64
	if err := f.db.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type='legacy_value_baseline'", f.sub.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
