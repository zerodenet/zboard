package datastore_test

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func TestSQLiteEntitlementFundingMigrationRecovery(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "funding.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	assertEntitlementFundingMigration(t, db)
}
func TestMySQLEntitlementFundingMigrationRecovery(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertEntitlementFundingMigration(t, db)
}
func assertEntitlementFundingMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	const version = "0049_entitlement_funding.up.sql"
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn("order_fundings", "entitlement_minor") {
		t.Fatal("fresh schema missing deferred contribution")
	}
	user := model.User{Email: "funding-schema@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "funding-schema", Code: "funding-schema", IsEnabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "funding-schema", Slug: "funding-schema", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "funding-schema", PriceCents: 100, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", IsActive: true}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	order := model.Order{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: "funding-schema-preserved", Status: "pending", AmountCents: 100, PayableAmount: 100, Currency: "CNY", Channel: "wallet"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	fund := model.OrderFunding{OrderID: order.ID, UserID: user.ID, Currency: "CNY", UseBalance: true, AmountMinor: 100, WalletMinor: 50, CashMinor: 50, State: "reserved", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&fund).Error; err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE orders MODIFY COLUMN change_snapshot VARCHAR(4096) NOT NULL DEFAULT ''").Error; err != nil {
			t.Fatal(err)
		}
	}
	// An old schema has existing holds but no deferred-value column yet.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE order_fundings DROP CHECK ck_order_funding_entitlement").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropColumn("order_fundings", "entitlement_minor"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("old schema upgrade", err)
	}
	var upgraded model.OrderFunding
	if err := db.First(&upgraded, "order_id = ?", order.ID).Error; err != nil || upgraded.EntitlementMinor != 0 || upgraded.WalletMinor != 50 || upgraded.CashMinor != 50 || upgraded.State != "reserved" || !upgraded.ExpiresAt.Equal(fund.ExpiresAt) {
		t.Fatal("upgrade changed existing financial hold", upgraded, err)
	}
	if err := db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("entitlement_minor", int64(37)).Error; err != nil {
		t.Fatal(err)
	}
	// Complete source maps may exceed the old VARCHAR(4096) contract. The
	// formal upgrade and interrupted recovery must retain the whole evidence.
	largeSnapshot := `{"evidence":"` + strings.Repeat("a", 12000) + `"}`
	if err := db.Model(&model.Order{}).Where("id = ?", order.ID).Update("change_snapshot", largeSnapshot).Error; err != nil {
		t.Fatal("full source snapshot truncated by old schema", err)
	}
	// Interrupt after DDL but before recording the migration. Preserve the
	// source column and let startup safely reconcile the remaining check/record.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE order_fundings DROP CHECK ck_order_funding_entitlement").Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("partial DDL/repeat recovery", err)
		}
	}
	if db.Dialector.Name() == "mysql" && !db.Migrator().HasConstraint("order_fundings", "ck_order_funding_entitlement") {
		t.Fatal("missing recovered funding invariant")
	}
	var retained model.OrderFunding
	if err := db.First(&retained, "order_id = ?", order.ID).Error; err != nil || retained.EntitlementMinor != 37 || retained.WalletMinor != 50 || retained.CashMinor != 50 || retained.State != "reserved" {
		t.Fatal("interrupted/repeated migration changed a live hold", retained, err)
	}
	var preservedOrder model.Order
	if err := db.First(&preservedOrder, order.ID).Error; err != nil || preservedOrder.ChangeSnapshot != largeSnapshot {
		t.Fatal("source evidence changed during schema recovery", err)
	}
	// Once recorded, losing this source cannot be repaired by defaulting a
	// live entitlement contribution to zero. Fail before automatic recovery.
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE order_fundings DROP CHECK ck_order_funding_entitlement").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropColumn("order_fundings", "entitlement_minor"); err != nil {
		t.Fatal(err)
	}
	err := datastore.RunMigrations(db)
	if err == nil || !strings.Contains(err.Error(), "restore before startup") {
		t.Fatal("lost recorded money source silently recreated", err)
	}
	if db.Migrator().HasColumn("order_fundings", "entitlement_minor") {
		t.Fatal("preflight changed source before refusing startup")
	}
}
