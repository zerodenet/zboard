package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPurchaseSnapshotSurvivesCatalogEditsAndEarlyRenewal(t *testing.T) {
	assertPurchaseSnapshot(t, newOrderFixture(t))
}
func TestMySQLPurchaseSnapshotSurvivesCatalogEditsAndEarlyRenewal(t *testing.T) {
	assertPurchaseSnapshot(t, newMySQLBusinessOrderFixture(t))
}
func assertPurchaseSnapshot(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"reset_policy": 2, "traffic_calc_mode": 1, "family_limit": 3}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("renewal_effect", "extend_only").Error; err != nil {
		t.Fatal(err)
	}
	pending := f.create(t, 0)
	// Internal evidence is deliberately absent from the HTTP order projection.
	if err := f.h.db.First(&pending, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot commerce.PurchaseSnapshot
	if err := json.Unmarshal([]byte(pending.PurchaseSnapshot), &snapshot); err != nil || snapshot.Version != 1 || !snapshot.Renewable || snapshot.ResetPolicy != 2 || snapshot.FamilyLimit != 3 || snapshot.TrafficCalcMode != 1 {
		t.Fatalf("purchase policy not captured: %+v %v", snapshot, err)
	}
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"reset_policy": 4, "traffic_calc_mode": 2, "family_limit": 7, "is_renewable": false}).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, pending.ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.ResetPolicy != 2 || sub.TrafficCalcMode != 1 || sub.FamilyLimit != 3 || sub.Lifecycle != "renewable" {
		t.Fatalf("live catalog replaced purchased policy: %+v", sub)
	}
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Update("renewal_effect", "extend_and_add_quota").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&sub).Update("flow_used", 60).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: sub.ID}
	quote, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = quote.QuoteFingerprint
	if err := f.h.db.Model(&f.planRecord).Update("family_limit", 8).Error; err != nil {
		t.Fatal(err)
	}
	var invalid *commerce.ValidationError
	if _, err := f.h.services.OrderCreation.Create(t.Context(), 1, request); !errors.As(err, &invalid) {
		t.Fatalf("changed policy quote accepted: %v", err)
	}
	request.QuoteFingerprint = ""
	renewal, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if renewal.RenewalEffect != "extend_only" {
		t.Fatal("new timed renewal inherited the legacy quota-addition effect")
	}
	f.paid(t, renewal.ID)
	var renewed model.Subscription
	if err := f.h.db.First(&renewed, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if renewed.ResetPolicy != sub.ResetPolicy || renewed.TrafficCalcMode != sub.TrafficCalcMode || sub.NextResetAt == nil || renewed.NextResetAt == nil || !renewed.NextResetAt.Equal(*sub.NextResetAt) {
		t.Fatalf("early renewal moved quota policy/schedule: before=%+v after=%+v", sub, renewed)
	}
	if renewed.FlowUsed != 60 || renewed.FlowTotal != sub.FlowTotal {
		t.Fatal("early renewal reset consumption or added immediate quota")
	}
	legacyRenewal, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", legacyRenewal.ID).Updates(map[string]any{"purchase_snapshot": "", "renewal_effect": "extend_and_add_quota"}).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, legacyRenewal.ID)
	var legacyRenewed model.Subscription
	if err := f.h.db.First(&legacyRenewed, sub.ID).Error; err != nil || legacyRenewed.FlowTotal != renewed.FlowTotal+legacyRenewal.TrafficBytes || legacyRenewed.FlowUsed != renewed.FlowUsed {
		t.Fatalf("pre-upgrade pending renewal lost its committed effect: %+v %v", legacyRenewed, err)
	}
	bad := f.create(t, 0)
	if err := f.h.db.Model(&bad).Update("purchase_snapshot", `{"version":99}`).Error; err != nil {
		t.Fatal(err)
	}
	if out := f.pay(t, bad.ID, false); out.Code != http.StatusBadRequest {
		t.Fatalf("unsupported snapshot fell back to live policy: %d %s", out.Code, out.Body.String())
	}
	var actual model.Order
	if err := f.h.db.First(&actual, bad.ID).Error; err != nil || actual.Status != "pending" || actual.FulfilledAt != nil {
		t.Fatalf("invalid snapshot committed: %+v %v", actual, err)
	}
	legacy := f.create(t, 0)
	if err := f.h.db.Model(&legacy).Update("purchase_snapshot", "").Error; err != nil {
		t.Fatal(err)
	}
	legacy = f.paid(t, legacy.ID)
	if legacy.PurchaseSnapshot != "" {
		t.Fatal("legacy policy was falsely backfilled as a purchase snapshot")
	}
}

func TestSQLitePurchaseSnapshotMigrationAndRecovery(t *testing.T) {
	for _, state := range []string{"repeat", "interrupted_after_ddl", "old_schema", "recorded_missing_column"} {
		t.Run(state, func(t *testing.T) { assertPurchaseSnapshotMigration(t, newOrderFixture(t), state) })
	}
}
func TestMySQLPurchaseSnapshotMigrationAndRecovery(t *testing.T) {
	for _, state := range []string{"repeat", "interrupted_after_ddl", "old_schema", "recorded_missing_column"} {
		t.Run(state, func(t *testing.T) { assertPurchaseSnapshotMigration(t, newMySQLBusinessOrderFixture(t), state) })
	}
}
func assertPurchaseSnapshotMigration(t *testing.T, f orderFixture, state string) {
	t.Helper()
	order := f.paid(t, f.create(t, 0).ID)
	const version = "0037_commercial_purchase_snapshot.up.sql"
	// These cases model the pre-0055 legacy snapshot schema. A completed 0055
	// can contain accepted discount evidence and deliberately refuses a lost
	// column before AutoMigrate (covered separately on SQLite and real MySQL).
	// Keep 0037's recorded-missing-column recovery assertion unchanged.
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0055_discount_purchase_snapshot.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if state == "interrupted_after_ddl" || state == "old_schema" {
		if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
			t.Fatal(err)
		}
	}
	if state == "old_schema" || state == "recorded_missing_column" {
		if err := f.h.db.Migrator().DropColumn(&model.Order{}, "PurchaseSnapshot"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.AmountCents != order.AmountCents || actual.PaidAmount != order.PaidAmount || actual.Status != "paid" || actual.SubscriptionID != order.SubscriptionID {
		t.Fatalf("migration rewrote paid history: %+v %v", actual, err)
	}
	if state == "old_schema" || state == "recorded_missing_column" {
		if actual.PurchaseSnapshot != "" {
			t.Fatal("schema recovery invented a missing historical policy")
		}
	} else if actual.PurchaseSnapshot != order.PurchaseSnapshot {
		t.Fatal("repeated/interrupted migration rewrote an existing snapshot")
	}
	var count int64
	if err := f.h.db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("migration receipt: %d %v", count, err)
	}
}
