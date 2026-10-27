package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestOversizedEvidenceStillAllowsExplicitZeroWholeTermination(t *testing.T) {
	assertOversizedZeroTermination(t, newOrderFixture(t))
}
func TestMySQLOversizedEvidenceStillAllowsExplicitZeroWholeTermination(t *testing.T) {
	assertOversizedZeroTermination(t, newMySQLBusinessOrderFixture(t))
}

func assertOversizedZeroTermination(t *testing.T, base orderFixture) {
	t.Helper()
	f, root := terminationHistoryFixture(t, base)
	unknown := strings.Repeat("未核对", 130000) // >1MiB by bytes; unknown legacy evidence.
	if err := f.h.db.Model(&root).Update("entitlement_value_snapshot", unknown).Error; err != nil {
		t.Fatal(err)
	}
	var before model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	var beforeEntries int64
	if err := f.h.db.Model(&model.WalletEntry{}).Count(&beforeEntries).Error; err != nil {
		t.Fatal(err)
	}
	const guard = "test:oversized-zero-no-body"
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(guard, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" {
			return
		}
		selection := strings.Join(tx.Statement.Selects, ",")
		if selection == "id" || strings.Contains(selection, "AS evidence_bytes") {
			return
		}
		omitted := map[string]bool{}
		for _, name := range tx.Statement.Omits {
			omitted[name] = true
		}
		if omitted["purchase_snapshot"] && omitted["entitlement_value_snapshot"] && omitted["change_snapshot"] {
			return
		}
		tx.AddError(errors.New("oversized paid evidence body was loaded"))
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Query().Remove(guard) })
	q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, root.SubscriptionID)
	if err != nil || q.ValuationStatus != "unavailable" || q.TotalMinor != nil || q.RootOrderID != nil || !strings.Contains(q.Message, "预算") {
		t.Fatalf("budget became a price: %+v %v", q, err)
	}
	positive := int64(50)
	for _, amount := range []*int64{nil, &positive} {
		_, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, root.SubscriptionID, commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "oversized-reject-default"}, AmountMinor: amount, Reason: "unknown cash must never default to zero or a positive refund"})
		if !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
			t.Fatalf("unpriced refund accepted: %v", err)
		}
	}
	zero := int64(0)
	in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "oversized-explicit-zero"}, AmountMinor: &zero, Reason: "administrator explicitly ends all remaining rights without refund"}
	var receipt uint
	for range 2 {
		r, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, root.SubscriptionID, in)
		if err != nil || r.RecordKind != "entitlement_mutation" || r.AmountMinor != 0 || receipt != 0 && receipt != r.ID {
			t.Fatalf("explicit zero whole termination %+v %v", r, err)
		}
		receipt = r.ID
	}
	if err := f.h.db.Callback().Query().Remove(guard); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil {
		t.Fatalf("rights survived %+v %v", sub, err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil || root.EntitlementValueSnapshot != unknown || root.RefundAmount != 0 || root.PaidAmount != 10000 {
		t.Fatalf("historical evidence/cash changed %+v %v", root.ID, err)
	}
	var after model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", before.UserID, before.Currency).First(&after).Error; err != nil || after.BalanceMinor != before.BalanceMinor || after.ReservedMinor != before.ReservedMinor || after.Revision != before.Revision {
		t.Fatalf("zero path changed money %+v %v", after, err)
	}
	var count int64
	if err := f.h.db.Model(&model.WalletEntry{}).Count(&count).Error; err != nil || count != beforeEntries {
		t.Fatalf("zero path created money entry %d %v", count, err)
	}
}
