package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strings"
	"testing"
	"time"
)

func readValueEvidence(t testing.TB, f orderFixture, id uint) commerce.EntitlementValueSnapshot {
	t.Helper()
	var order model.Order
	if err := f.h.db.First(&order, id).Error; err != nil {
		t.Fatal(err)
	}
	var value commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(order.EntitlementValueSnapshot), &value); err != nil {
		t.Fatalf("missing actual fulfillment evidence: %q %v", order.EntitlementValueSnapshot, err)
	}
	return value
}
func TestActualEntitlementValueCapture(t *testing.T) {
	assertActualEntitlementValueCapture(t, newOrderFixture(t))
}
func TestMySQLActualEntitlementValueCapture(t *testing.T) {
	assertActualEntitlementValueCapture(t, newMySQLOrderFixture(t))
}
func assertActualEntitlementValueCapture(t *testing.T, f orderFixture) {
	priceForFunding(t, f, 12000)
	order := f.create(t, 0)
	provider := "value-source-channel"
	prepareReceiptAttempt(t, f, order.ID, provider)
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "actual-value-payment", ProviderTradeNo: "actual-value-trade", Status: "paid", AmountMinor: 12000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}
	receipt, err := receiptService(f).Record(t.Context(), authority, fact)
	if err != nil {
		t.Fatal(err)
	}
	value := readValueEvidence(t, f, order.ID)
	if value.Version != 1 || value.QuotaEventID == 0 || value.SubscriptionID == 0 || !receipt.Fulfilled || value.OrderID != order.ID || !value.PaymentVerified || value.ActualPaidMinor != 12000 || value.Mode != "new_term" || value.Before != nil || len(value.PaidPeriods) != 1 || !value.TermStartAt.Equal(value.After.StartAt) || !value.TermEndAt.Equal(value.After.EndAt) {
		t.Fatalf("wrong actual paid facts: %+v", value)
	}
	original, _ := json.Marshal(value)
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", 24000).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := receiptService(f).Record(t.Context(), authority, fact); err != nil {
		t.Fatal(err)
	}
	again := readValueEvidence(t, f, order.ID)
	replay, _ := json.Marshal(again)
	if string(original) != string(replay) {
		t.Fatal("payment replay or catalog edit rewrote paid value evidence")
	}
	// A manual grant remains a manual record; it is not invented verified cash.
	manual := f.create(t, 0)
	if out := f.pay(t, manual.ID, false); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	manualValue := readValueEvidence(t, f, manual.ID)
	if manualValue.PaymentVerified || manualValue.PaymentBasis != "manual_record_unverified" || len(manualValue.PaidPeriods) != 0 {
		t.Fatalf("manual assignment invented verified cash: %+v", manualValue)
	}
	raw, _ := json.Marshal(commerce.Order(model.Order{EntitlementValueSnapshot: string(original)}))
	if strings.Contains(string(raw), "entitlement_value_snapshot") || strings.Contains(string(raw), "payment_verified") {
		t.Fatal("private financial snapshot leaked in order HTTP DTO")
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	retained := readValueEvidence(t, f, order.ID)
	retainedJSON, _ := json.Marshal(retained)
	if string(retainedJSON) != string(original) {
		t.Fatal("migration rewrote financial evidence")
	}
}
func TestEntitlementValueSnapshotLossFailsBeforeRecreation(t *testing.T) {
	assertEntitlementValueLoss(t, newOrderFixture(t))
}
func TestMySQLEntitlementValueSnapshotLossFailsBeforeRecreation(t *testing.T) {
	assertEntitlementValueLoss(t, newMySQLOrderFixture(t))
}
func assertEntitlementValueLoss(t *testing.T, f orderFixture) {
	if err := f.h.db.Exec("ALTER TABLE orders DROP COLUMN entitlement_value_snapshot").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("lost financial evidence replaced by empty values")
	}
	if f.h.db.Migrator().HasColumn("orders", "entitlement_value_snapshot") {
		t.Fatal("lost financial evidence column silently recreated")
	}
	// Only an explicitly unrecorded additive DDL is recoverable here. This test
	// fixture has no paid evidence; operators must not delete receipts on live DBs.
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0042_entitlement_value_snapshot.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	if !f.h.db.Migrator().HasColumn("orders", "entitlement_value_snapshot") {
		t.Fatal("empty interrupted schema not completed")
	}
}

func TestEntitlementValueEvidenceFailureRollsBackPayment(t *testing.T) {
	assertValueEvidenceFailure(t, newOrderFixture(t))
}
func TestMySQLEntitlementValueEvidenceFailureRollsBackPayment(t *testing.T) {
	assertValueEvidenceFailure(t, newMySQLBusinessOrderFixture(t))
}
func assertValueEvidenceFailure(t *testing.T, f orderFixture) {
	order := f.create(t, 0)
	provider := "value-rollback-channel"
	prepareReceiptAttempt(t, f, order.ID, provider)
	sql := `CREATE TRIGGER reject_value_capture BEFORE UPDATE OF entitlement_value_snapshot ON orders BEGIN SELECT RAISE(ABORT, 'financial evidence unavailable'); END`
	if !datastore.IsSQLite(f.h.db) {
		sql = `CREATE TRIGGER reject_value_capture BEFORE UPDATE ON orders FOR EACH ROW BEGIN IF NEW.entitlement_value_snapshot <> OLD.entitlement_value_snapshot THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'financial evidence unavailable'; END IF; END`
	}
	if err := f.h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "value-rollback-event", ProviderTradeNo: "value-rollback-trade", Status: "paid", AmountMinor: 100, Currency: "CNY", OccurredAt: time.Now().UTC()}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}
	if _, err := receiptService(f).Record(t.Context(), authority, fact); err == nil {
		t.Fatal("financial evidence loss acknowledged")
	}
	for _, table := range []string{"payment_events", "payment_receipts", "subscriptions", "protocol_credentials", "quota_events", "wallet_entries"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("partial %s: %d %v", table, n, err)
		}
	}
	var original model.Order
	if err := f.h.db.First(&original, order.ID).Error; err != nil || original.Status != "pending" || original.EntitlementValueSnapshot != "" {
		t.Fatalf("partial paid state %+v %v", original, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_value_capture").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := receiptService(f).Record(t.Context(), authority, fact); err != nil {
		t.Fatal(err)
	}
	value := readValueEvidence(t, f, order.ID)
	if !value.PaymentVerified || value.ActualPaidMinor != 100 {
		t.Fatalf("retry did not capture cash basis: %+v", value)
	}
	var stored model.Subscription
	if err := f.h.db.First(&stored, value.SubscriptionID).Error; err != nil || !stored.StartAt.Equal(value.TermStartAt) || !stored.EndAt.Equal(value.TermEndAt) || len(value.PaidPeriods) != 1 {
		t.Fatalf("persisted precision or periods differ: %+v %+v %v", stored, value, err)
	}
}

func TestRenewalAndResetCaptureActualBeforeAndAfter(t *testing.T) {
	assertRenewalAndResetValueCapture(t, newOrderFixture(t))
}
func TestMySQLRenewalAndResetCaptureActualBeforeAndAfter(t *testing.T) {
	assertRenewalAndResetValueCapture(t, newMySQLBusinessOrderFixture(t))
}
func assertRenewalAndResetValueCapture(t *testing.T, f orderFixture) {
	if err := f.h.db.Model(&f.planRecord).Update("reset_policy", 2).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 12000)
	fundTestWallet(t, f, 100000)
	pay := func(order model.Order) {
		t.Helper()
		s := fundingService(f)
		if _, err := s.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	first := f.create(t, 0)
	pay(first)
	purchase := readValueEvidence(t, f, first.ID)
	var before model.Subscription
	if err := f.h.db.First(&before, purchase.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&before).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	renewal := f.create(t, before.ID)
	pay(renewal)
	extended := readValueEvidence(t, f, renewal.ID)
	if !extended.PaymentVerified || extended.QuotaEventID <= purchase.QuotaEventID || extended.Mode != "extended_term" || extended.Before == nil || extended.Before.FlowUsed != 80 || extended.After.FlowUsed != 80 || !extended.TermStartAt.Equal(before.EndAt) || !extended.TermEndAt.Equal(extended.After.EndAt) || len(extended.PaidPeriods) != 1 || extended.Before.NextResetAt == nil || extended.After.NextResetAt == nil || !extended.Before.NextResetAt.Equal(*extended.After.NextResetAt) {
		t.Fatalf("renewal valuation reconstructed or reset usage: %+v", extended)
	}
	f = prepareResetSKU(t, f)
	reset := f.create(t, before.ID)
	pay(reset)
	resetValue := readValueEvidence(t, f, reset.ID)
	if !resetValue.PaymentVerified || resetValue.QuotaEventID <= extended.QuotaEventID || resetValue.ActualPaidMinor != 500 || resetValue.Mode != "paid_reset" || resetValue.Before == nil || resetValue.Before.FlowUsed != 80 || resetValue.After.FlowUsed != 80 || resetValue.After.CycleStartUsed != 80 || resetValue.After.FlowTotal != 80+resetValue.After.ResetQuotaBytes || !resetValue.Before.EndAt.Equal(resetValue.After.EndAt) || resetValue.Before.NextResetAt == nil || resetValue.After.NextResetAt == nil || !resetValue.Before.NextResetAt.Equal(*resetValue.After.NextResetAt) || !resetValue.TermEndAt.Equal(*resetValue.After.NextResetAt) {
		t.Fatalf("paid reset missing real value baseline or moved its deadline: %+v", resetValue)
	}
}
