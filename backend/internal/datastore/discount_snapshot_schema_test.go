package datastore_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteDiscountSnapshotMigrationPreservesEvidence(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "discount-evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertDiscountSnapshotMigration(t, db)
}

func TestMySQLDiscountSnapshotMigrationPreservesEvidence(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertDiscountSnapshotMigration(t, db)
}

func assertDiscountSnapshotMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	const version = "0055_discount_purchase_snapshot.up.sql"
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("fresh schema", err)
	}
	user := model.User{Email: "discount-schema@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "discount-schema", Code: "discount-schema", IsEnabled: true}
	for _, row := range []any{&user, &group} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "discount-schema", Slug: "discount-schema", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "discount-schema", PriceCents: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", IsActive: true}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	// Preserve unknown old bytes exactly. A migration cannot turn missing or
	// malformed historic purchase facts into a current-price agreement.
	legacy := "legacy-unverified-购买事实"
	order := model.Order{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: "discount-schema", Status: "pending", AmountCents: 12000, PayableAmount: 10000, DiscountAmount: 2000, Currency: "CNY", Channel: "wallet", PurchaseSnapshot: legacy}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if !datastore.IsSQLite(db) {
		if err := db.Exec("ALTER TABLE orders MODIFY COLUMN purchase_snapshot VARCHAR(2048) NOT NULL DEFAULT ''").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("old schema upgrade", err)
	}
	assertBytes := func(want string) {
		t.Helper()
		var got model.Order
		if err := db.First(&got, order.ID).Error; err != nil || got.PurchaseSnapshot != want || got.AmountCents != 12000 || got.DiscountAmount != 2000 || got.PayableAmount != 10000 || got.Status != "pending" {
			t.Fatalf("migration changed immutable evidence or business state: %v", err)
		}
	}
	assertBytes(legacy)
	large := `{"opaque_terms":"` + strings.Repeat("完整条款😀", 1000) + `"}`
	if err := db.Model(&order).Update("purchase_snapshot", large).Error; err != nil {
		t.Fatal("larger evidence rejected after expansion", err)
	}
	// DDL can commit before its receipt. Retry and repeated startup must retain
	// the full byte string, and never replay old 0037 VARCHAR sizing.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("interrupted/repeated migration", err)
		}
		assertBytes(large)
	}
	if !datastore.IsSQLite(db) {
		if err := db.Exec("ALTER TABLE orders MODIFY COLUMN purchase_snapshot MEDIUMTEXT NOT NULL DEFAULT ('')").Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
		var kind string
		if err := db.Raw("SELECT DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'orders' AND COLUMN_NAME = 'purchase_snapshot'").Scan(&kind).Error; err != nil || kind != "mediumtext" {
			t.Fatal("existing wider column shrank", kind, err)
		}
		assertBytes(large)
	}
	// A completed migration means this column could hold financial evidence.
	// Fail before AutoMigrate rather than fabricating empty historical facts.
	if err := db.Migrator().DropColumn("orders", "purchase_snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "restore before startup") {
		t.Fatal("lost purchase evidence silently recreated", err)
	}
	if db.Migrator().HasColumn("orders", "purchase_snapshot") {
		t.Fatal("preflight recreated missing financial evidence")
	}
}
