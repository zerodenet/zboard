package datastore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteMigrationsCreateCompleteApplicationInventory(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "zboard.db"))
	if err != nil {
		t.Fatalf("OpenWithDriver() error = %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() error = %v", err)
	}
	defer sqlDB.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	tables, err := MigrationTables(db)
	if err != nil {
		t.Fatalf("MigrationTables() error = %v", err)
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			t.Errorf("SQLite schema is missing migration table %q", table)
		}
	}
	if !db.Migrator().HasTable("schema_migrations") {
		t.Error("SQLite schema is missing schema_migrations")
	}
	if err := ReconcileTrafficReadSchema(db); err != nil {
		t.Fatalf("ReconcileTrafficReadSchema() error = %v", err)
	}
	for _, index := range trafficReadIndexes {
		if !db.Migrator().HasIndex(index.table, index.name) {
			t.Errorf("SQLite schema is missing traffic read index %q", index.name)
		}
	}
}

func TestSQLiteMigrationInventoryHasNoDuplicates(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	tables, err := MigrationTables(db)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, table := range tables {
		if seen[table] {
			t.Fatalf("duplicate migration table %q", table)
		}
		seen[table] = true
	}
}

func TestSQLiteMigrationBackfillsResetQuotaFromPaidPlanOrder(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{Email: "reset@example.test", Password: "hash", Status: "active"}
	group := model.NodeGroup{Name: "Reset", Code: "reset", IsEnabled: true}
	for _, item := range []any{&user, &group} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "Plan", Slug: "reset-plan", NodeGroupID: group.ID, TrafficBytes: 5000, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "reset-monthly", Name: "Monthly", BillingUnit: "month", BillingValue: 1, Currency: "CNY", IsActive: true}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID,
		StartAt: now, EndAt: now.AddDate(0, 2, 0), Status: "active", FlowTotal: 150,
		ResetPolicy: 2, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	baseOrder := model.Order{UserID: user.ID, SubscriptionID: sub.ID, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: "reset-base", OrderType: "new", Status: "paid", PlanName: plan.Name, SKUName: sku.Name,
		BillingUnit: "month", BillingValue: 1, TrafficBytes: 100, FulfilledAt: &now}
	addonTime := now.Add(time.Minute)
	addonOrder := model.Order{UserID: user.ID, SubscriptionID: sub.ID, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: "reset-addon", OrderType: "traffic_pack", Status: "paid", PlanName: plan.Name, SKUName: sku.Name,
		BillingUnit: "once", BillingValue: 1, TrafficBytes: 50, FulfilledAt: &addonTime}
	for _, item := range []any{&baseOrder, &addonOrder} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.ResetQuotaBytes != 100 {
		t.Fatalf("backfilled reset quota = %d, want paid plan order snapshot 100", sub.ResetQuotaBytes)
	}
}
