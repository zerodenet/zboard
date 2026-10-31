package handler

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestAccountDeletionRetainsIndefiniteLateMetering(t *testing.T) {
	assertAccountDeletionLateMetering(t, newOrderFixture(t))
}
func TestMySQLAccountDeletionRetainsIndefiniteLateMetering(t *testing.T) {
	assertAccountDeletionLateMetering(t, newMySQLBusinessOrderFixture(t))
}
func assertAccountDeletionLateMetering(t *testing.T, f orderFixture) {
	t.Helper()
	h, credential := accountingFixtureFromOrder(t, f)
	admin := model.User{Email: "deleting-admin@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id=?", credential.UserID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 1)
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := h.db.Model(&model.Subscription{}).Where("id=?", credential.SubscriptionID).Update("end_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.services.CredentialExpiry(h.credentialCipher).ExpireDue(t.Context(), now, 20); err != nil {
		t.Fatal(err)
	}
	preview, err := h.services.AccountDeletion().Preview(t.Context(), admin.ID, credential.UserID)
	if err != nil || !preview.Allowed {
		t.Fatal(preview, err)
	}
	if err := h.services.AccountDeletion().Delete(t.Context(), admin.ID, credential.UserID); err != nil {
		t.Fatal(err)
	}
	late := accountingBenchmarkEvents(credential, 1, 1)
	// Retained identity accepts a new report long after the original event;
	// duplicate delivery remains idempotent and cannot revive the entitlement.
	for i := 0; i < 2; i++ {
		if err := h.projectZeroNodeEvents(t.Context(), late); err != nil {
			t.Fatal(err)
		}
	}
	var cursor model.FlowUsage
	if err := h.db.Where("subscription_id=?", credential.SubscriptionID).First(&cursor).Error; err != nil || cursor.RawBytes != 60 || cursor.UsedBytes != 60 {
		t.Fatalf("late cursor: %+v %v", cursor, err)
	}
	var raw, used int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id=? AND user_id=?", credential.SubscriptionID, credential.UserID).Select("COALESCE(SUM(raw_bytes),0)").Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id=? AND user_id=?", credential.SubscriptionID, credential.UserID).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || raw != 60 || used != 60 {
		t.Fatal("deleted account lost/doubled history", raw, used, err)
	}
	var count int64
	h.db.Unscoped().Model(&model.User{}).Where("id=?", credential.UserID).Count(&count)
	if count != 0 {
		t.Fatal("account survived")
	}
}
