package datastore_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteTicketReferenceMigrationPreservesHistoryAndRefusesLostContext(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "ticket-references.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertTicketReferenceMigration(t, db)
}

func TestMySQLTicketReferenceMigrationPreservesHistoryAndRefusesLostContext(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertTicketReferenceMigration(t, db)
}

func assertTicketReferenceMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	const receipt = "0058_ticket_references.up.sql"
	columns := []string{"order_id", "subscription_id", "wallet_entry_id"}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	user := model.User{Email: "ticket-migration@example.test", Password: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	legacy := model.Ticket{TicketNo: "T-MIGRATION-LEGACY", UserID: user.ID, Subject: "Retained historical support", Category: "other", Priority: 1, Status: "open", LastMessageAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	assertLegacy := func() {
		t.Helper()
		var row model.Ticket
		if err := db.First(&row, legacy.ID).Error; err != nil || row.Subject != legacy.Subject || row.TicketNo != legacy.TicketNo || row.UserID != legacy.UserID || row.OrderID != nil || row.SubscriptionID != nil || row.WalletEntryID != nil {
			t.Fatalf("legacy support context fabricated or history changed: %+v %v", row, err)
		}
	}
	forgetReceipt := func() {
		t.Helper()
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
			t.Fatal(err)
		}
	}
	drop := func(names ...string) {
		t.Helper()
		for _, name := range names {
			if err := db.Migrator().DropColumn("tickets", name); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A pre0058 instance has no reference authority. Both formal migration
	// assets execute against the real driver before normal reconciliation.
	forgetReceipt()
	drop(columns...)
	asset := receipt
	if datastore.IsSQLite(db) {
		asset = "sqlite/0058_ticket_references.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal("formal ticket reference DDL", err)
			}
		}
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	assertLegacy()
	// Interrupted empty partial DDL can complete, but its receipt must not be
	// fabricated when recording fails after the columns have been prepared.
	forgetReceipt()
	drop("subscription_id", "wallet_entry_id")
	const callback = "test:reject_ticket_reference_receipt"
	injected := errors.New("injected ticket reference receipt failure")
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "schema_migrations" {
			return
		}
		value := reflect.Indirect(reflect.ValueOf(tx.Statement.Dest))
		if value.IsValid() && value.Kind() == reflect.Struct {
			version := value.FieldByName("Version")
			if version.IsValid() && version.Kind() == reflect.String && version.String() == receipt {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	if err := datastore.RunMigrations(db); !errors.Is(err, injected) {
		t.Fatalf("receipt interruption hidden: %v", err)
	}
	for _, column := range columns {
		if !db.Migrator().HasColumn("tickets", column) {
			t.Fatal("partial schema was not completed before receipt failure", column)
		}
	}
	var recorded int64
	if err := db.Table("schema_migrations").Where("version = ?", receipt).Count(&recorded).Error; err != nil || recorded != 0 {
		t.Fatal("failed receipt was recorded", recorded, err)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("repeated recovery", err)
		}
		assertLegacy()
	}
	// These IDs are schema-only historical fixture context. They are not payment
	// facts or permission evidence; HTTP ownership is tested separately.
	if err := db.Model(&legacy).Updates(map[string]any{"order_id": 17}).Error; err != nil {
		t.Fatal(err)
	}
	forgetReceipt()
	drop("wallet_entry_id")
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "incomplete ticket reference schema contains evidence") {
		t.Fatal("partial historical context was replaced with defaults", err)
	}
	if db.Migrator().HasColumn("tickets", "wallet_entry_id") {
		t.Fatal("schema repair ran before evidence preflight")
	}
	definition := "bigint unsigned"
	if datastore.IsSQLite(db) {
		definition = "integer"
	}
	// Restore only the fixture column that was NULL before deliberate loss.
	if err := db.Exec("ALTER TABLE tickets ADD COLUMN wallet_entry_id " + definition + " NULL DEFAULT NULL").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	drop("subscription_id")
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "ticket reference evidence missing") {
		t.Fatal("recorded history loss was silently repaired", err)
	}
	if db.Migrator().HasColumn("tickets", "subscription_id") {
		t.Fatal("recorded evidence loss was defaulted by AutoMigrate")
	}
	var retained struct{ OrderID *uint }
	if err := db.Table("tickets").Select("order_id").Where("id = ?", legacy.ID).Take(&retained).Error; err != nil || retained.OrderID == nil || *retained.OrderID != 17 {
		t.Fatal("surviving reference was changed", err)
	}
}
