package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRetirementRetainsLateMeteringIdentityAndReleasesReservations(t *testing.T) {
	assertRetirementMetering(t, newOrderFixture(t))
}
func TestMySQLRetirementRetainsLateMeteringIdentityAndReleasesReservations(t *testing.T) {
	assertRetirementMetering(t, newMySQLBusinessOrderFixture(t))
}
func assertRetirementMetering(t *testing.T, f orderFixture) {
	priceForFunding(t, f, 1000)
	h, credential := accountingFixtureFromOrder(t, f)
	fundTestWallet(t, f, 500)
	pending := f.create(t, credential.SubscriptionID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	const provider = "retirement-late-payment"
	prepareReceiptAttempt(t, f, pending.ID, provider)
	control := f.paid(t, f.create(t, 0).ID)
	plain, err := h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 1)
	events[0].PrincipalKey = plain
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	var cursor model.FlowUsage
	if err := h.db.Where("subscription_id = ?", credential.SubscriptionID).First(&cursor).Error; err != nil || cursor.UsedBytes != 30 {
		t.Fatalf("cursor %+v %v", cursor, err)
	}
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Updates(map[string]any{"lifecycle": "fixed", "ends_on_quota_exhaustion": true, "flow_total": 30}).Error; err != nil {
		t.Fatal(err)
	}
	sql := `CREATE TRIGGER reject_retirement_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription.cleanup' BEGIN SELECT RAISE(ABORT,'retirement audit unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		sql = `CREATE TRIGGER reject_retirement_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'subscription.cleanup' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'retirement audit unavailable'; END IF; END`
	}
	if err := h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	expire := func() error {
		_, err := h.services.CredentialExpiry(h.credentialCipher).ExpireDue(t.Context(), time.Now().UTC(), 20)
		return err
	}
	if err := expire(); err == nil {
		t.Fatal("audit failure committed retirement")
	}
	walletMatches(t, f, 500, 500, 2)
	var before model.ProtocolCredential
	if err := h.db.First(&before, credential.ID).Error; err != nil || before.Secret != credential.Secret || before.RetiredPrincipalHash != "" {
		t.Fatalf("failed cleanup destroyed matching evidence %+v %v", before, err)
	}
	if err := h.db.Exec("DROP TRIGGER reject_retirement_audit").Error; err != nil {
		t.Fatal(err)
	}
	if err := expire(); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 500, 0, 3)
	var retired model.ProtocolCredential
	if err := h.db.First(&retired, credential.ID).Error; err != nil || retired.Secret != "" || retired.Status != "revoked" || retired.RetiredPrincipalHash != entitlements.RetiredPrincipalHash(credential.NodeID, plain) {
		t.Fatalf("retired credential %+v %v", retired, err)
	}
	if _, err := meteringstore.ResolveCompletionCredential(h.db, h.credentialCipher, credential.NodeID+1000, plain); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("retired matching crossed node boundary %v", err)
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	late := accountingBenchmarkEvents(credential, 1, 1)
	late[0].PrincipalKey = plain
	for i := 0; i < 2; i++ {
		if err := h.projectZeroNodeEvents(t.Context(), late); err != nil {
			t.Fatal(err)
		}
	}
	var final model.FlowUsage
	if err := h.db.First(&final, cursor.ID).Error; err != nil || final.UsedBytes != 30 || final.RawBytes != 60 || final.ProtocolCredentialID != cursor.ProtocolCredentialID || final.PrincipalKey != credential.PrincipalKey {
		t.Fatalf("retirement lost cursor or double charged %+v %v", final, err)
	}
	var used int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id = ?", credential.SubscriptionID).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != 30 {
		t.Fatalf("historical ledger lost/doubled %d %v", used, err)
	}
	var raw int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id = ?", credential.SubscriptionID).Select("COALESCE(SUM(raw_bytes),0)").Scan(&raw).Error; err != nil || raw != 60 {
		t.Fatalf("late raw usage lost %d %v", raw, err)
	}
	var sub model.Subscription
	if err := h.db.First(&sub, credential.SubscriptionID).Error; err != nil || sub.Status != "ended" || sub.FlowUsed != 30 || sub.NextResetAt != nil || entitlements.CanRenewAt(entitlements.Subscription(sub), time.Now().UTC()) {
		t.Fatalf("late metering revived service %+v %v", sub, err)
	}
	var oldOrders []model.Order
	if err := h.db.Where("subscription_id = ? AND status = 'paid'", sub.ID).Find(&oldOrders).Error; err != nil || len(oldOrders) != 1 || oldOrders[0].SubscriptionFinalFlowUsed != 30 {
		t.Fatalf("late metering rewrote terminal summary %+v %v", oldOrders, err)
	}
	var records []model.TrafficRecord
	if err := h.db.Where("subscription_id = ?", sub.ID).Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if strings.Contains(record.Meta, plain) {
			t.Fatal("raw secret entered metering history")
		}
	}
	cash := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "retirement-late-cash", ProviderTradeNo: "retirement-late-cash", Status: "paid", AmountMinor: 500, Currency: "CNY", OccurredAt: time.Now().UTC()}
	for i := 0; i < 2; i++ {
		out, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, cash)
		if err != nil || out.Disposition != "wallet_credit" || out.Fulfilled {
			t.Fatalf("late cash revived retired subscription %+v %v", out, err)
		}
	}
	walletMatches(t, f, 1000, 0, 4)
	var independent model.Subscription
	if err := h.db.First(&independent, control.SubscriptionID).Error; err != nil || independent.Status != "active" || independent.FlowUsed != 0 {
		t.Fatalf("independent subscription affected %+v %v", independent, err)
	}
	var otherCredential model.ProtocolCredential
	if err := h.db.Where("subscription_id = ?", control.SubscriptionID).First(&otherCredential).Error; err != nil {
		t.Fatal(err)
	}
	shared, err := h.credentialCipher.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&otherCredential).Update("secret", shared).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := meteringstore.ResolveCompletionCredential(h.db, h.credentialCipher, credential.NodeID, plain); !errors.Is(err, metering.ErrPrincipalAmbiguous) {
		t.Fatalf("ambiguous credential charged an arbitrary owner %v", err)
	}
	if due, err := h.services.CredentialExpiry(h.credentialCipher).HasDue(context.Background(), time.Now().UTC()); err != nil || due {
		t.Fatalf("retirement did not converge %v %v", due, err)
	}
}
