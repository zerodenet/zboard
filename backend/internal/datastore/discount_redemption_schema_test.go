package datastore_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteDiscountRedemptionSchemaReplayCopyAndAuthorityLoss(t *testing.T) {
	assertDiscountRedemptionSchema(t, false)
}
func TestMySQLDiscountRedemptionSchemaReplayCopyAndAuthorityLoss(t *testing.T) {
	assertDiscountRedemptionSchema(t, true)
}
func assertDiscountRedemptionSchema(t *testing.T, mysql bool) {
	source := diagnosticSchemaDB(t, mysql)
	owner, sku := seedDiscountSchemaOwner(t, source)
	order := model.Order{UserID: owner.ID, PlanID: sku.PlanID, PlanSKUID: sku.ID, TradeNo: "discount-schema-fixture", Status: "pending", OrderType: "new", Currency: "CNY", AmountCents: 12000, DiscountAmount: 2000, PayableAmount: 10000, PurchaseSnapshot: "{}"}
	if err := source.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	binding := commerce.DiscountRedemptionBinding{Version: 1, Key: strings.Repeat("a", 64), OrderID: order.ID, BuyerID: owner.ID, InputFingerprint: strings.Repeat("b", 64), Provider: commerce.DiscountProviderIdentity{PluginID: "example.finite", SigningKeyFingerprint: strings.Repeat("c", 64), PackageDigest: strings.Repeat("d", 64), Generation: 1, ConfigRevision: 1}, AgreementID: "finite", AgreementVersion: "1", TermsHash: strings.Repeat("e", 64), BaseMinor: 12000, DiscountMinor: 2000, PayableMinor: 10000, Currency: "CNY"}
	hash, err := commerce.DiscountRedemptionHash(binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(binding)
	at := time.Now().UTC().Truncate(time.Millisecond)
	row := model.OrderDiscountRedemption{OrderID: order.ID, RedemptionKey: binding.Key, BindingHash: hash, BindingJSON: string(raw), ProviderID: binding.Provider.PluginID, Desired: "release", Observed: "unknown", Revision: 2, LeaseUntil: at, NextAttemptAt: at, CreatedAt: at, UpdatedAt: at}
	if err := source.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, terminal := range []string{"unknown", "committed", "released"} {
		if err := source.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", order.ID).Update("observed", terminal).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.ValidateDiscountRedemptionBinaryRollback(source); err == nil {
			t.Fatalf("%s treated as old decoder compatibility", terminal)
		}
	}
	if err := source.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", order.ID).Update("observed", row.Observed).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(source); err != nil {
			t.Fatal(err)
		}
	}
	var stored model.OrderDiscountRedemption
	if err := source.First(&stored, "order_id = ?", order.ID).Error; err != nil || stored.BindingJSON != row.BindingJSON || stored.Desired != "release" || stored.RedemptionKey != row.RedemptionKey {
		t.Fatalf("repeated migration changed recovery authority: %+v %v", stored, err)
	}
	target := diagnosticSchemaDB(t, mysql)
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal(err)
	}
	var copied model.OrderDiscountRedemption
	if err := target.First(&copied, "order_id = ?", order.ID).Error; err != nil || copied.BindingJSON != row.BindingJSON || copied.Desired != row.Desired || copied.Observed != row.Observed || copied.RedemptionKey != row.RedemptionKey {
		t.Fatalf("copy changed pending external intent: %+v %v", copied, err)
	}
	if err := source.Exec("DROP TABLE order_discount_redemptions").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil || !strings.Contains(err.Error(), "reservation authority missing") {
		t.Fatalf("lost financial authority was re-created: %v", err)
	}
	if source.Migrator().HasTable("order_discount_redemptions") {
		t.Fatal("missing recorded holds silently invented as empty")
	}
	if err := datastore.CopyApplicationData(source, target); err == nil {
		t.Fatal("copy skipped recorded external financial intent")
	}
	var retained int64
	if err := source.Model(&model.Order{}).Where("id = ?", order.ID).Count(&retained).Error; err != nil || retained != 1 {
		t.Fatalf("preflight refusal changed historical order: %d %v", retained, err)
	}
}

func TestSQLiteDiscountRedemptionAbsentAuthorityUsesSemanticBoundedSnapshotScan(t *testing.T) {
	assertDiscountSemanticObligationScan(t, false)
}
func TestMySQLDiscountRedemptionAbsentAuthorityUsesSemanticBoundedSnapshotScan(t *testing.T) {
	assertDiscountSemanticObligationScan(t, true)
}
func assertDiscountSemanticObligationScan(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	if err := db.Exec("DROP TABLE order_discount_redemptions").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0065_order_discount_redemptions.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	owner, sku := seedDiscountSchemaOwner(t, db)
	order := model.Order{UserID: owner.ID, PlanID: sku.PlanID, PlanSKUID: sku.ID, TradeNo: "semantic-discount-obligation", Status: "pending", PurchaseSnapshot: "{}"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	formats := []string{
		`{"discount":{"quote":{"offer":{"redemption":"exclusive_until_core_finalization_v1"}}}}`,
		"{\n  \"discount\": {\n    \"quote\": {\"offer\": { \"redemption\" : \"exclusive_until_core_finalization_v1\" }}\n  }\n}",
		`{"disco\u0075nt":{"qu\u006fte":{"offer":{"\u0072edemption":"excl\u0075sive_until_core_finalization_v1"}}}}`,
	}
	for _, snapshot := range formats {
		if err := db.Model(&model.Order{}).Where("id = ?", order.ID).Update("purchase_snapshot", snapshot).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "accepted discount reservation authority missing") {
			t.Fatalf("semantic obligation missed: %s %v", snapshot, err)
		}
		if db.Migrator().HasTable("order_discount_redemptions") {
			t.Fatal("unknown holds invented as empty table")
		}
	}
}

func TestSQLiteDiscountRedemptionRejectsPartialAuthorityAndQueueIndexes(t *testing.T) {
	db := diagnosticSchemaDB(t, false)
	for _, index := range []struct {
		name, columns string
		unique        bool
	}{
		{"idx_order_discount_redemption_key", "redemption_key", true},
		{"idx_order_discount_redemption_due", "next_attempt_at,order_id", false},
		{"idx_order_discount_redemption_expiry", "desired,created_at,order_id", false},
	} {
		if err := db.Exec("DROP INDEX " + index.name).Error; err != nil {
			t.Fatal(err)
		}
		prefix := "CREATE INDEX "
		if index.unique {
			prefix = "CREATE UNIQUE INDEX "
		}
		if err := db.Exec(prefix + index.name + " ON order_discount_redemptions(" + index.columns + ") WHERE desired = 'reserve'").Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "index must be complete") {
			t.Fatalf("partial index accepted: %s %v", index.name, err)
		}
		if err := db.Exec("DROP INDEX " + index.name).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(prefix + index.name + " ON order_discount_redemptions(" + index.columns + ")").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
}

func seedDiscountSchemaOwner(t *testing.T, db *gorm.DB) (model.User, model.PlanSKU) {
	t.Helper()
	user := model.User{Email: "discount-schema@example.test", AccountName: "discount-schema", Password: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "discount-schema", IsEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "discount-schema", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "discount-schema-month", Name: "month", SKUType: "new", BillingMode: "periodic", EntitlementMode: "plan", RenewalEffect: "extend_only", BillingUnit: "month", BillingValue: 1, PriceCents: 12000, Currency: "CNY", IsActive: true}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	return user, sku
}
