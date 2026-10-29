package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
)

func TestEntitlementEpochFormalAndInterruptedUpgradePreservesSources(t *testing.T) {
	for _, mode := range []string{"formal", "partial_columns", "partial_index"} {
		t.Run(mode, func(t *testing.T) { assertEpochUpgrade(t, newOrderFixture(t), mode) })
	}
}
func TestMySQLEntitlementEpochFormalAndInterruptedUpgradePreservesSources(t *testing.T) {
	for _, mode := range []string{"formal", "partial_columns", "partial_index"} {
		t.Run(mode, func(t *testing.T) { assertEpochUpgrade(t, newMySQLBusinessOrderFixture(t), mode) })
	}
}
func assertEpochUpgrade(t *testing.T, f orderFixture, mode string) {
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	db := f.h.db
	var original model.ProtocolCredential
	if err := db.Where("subscription_id = ?", paid.SubscriptionID).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	// Recreate the immediately previous schema from a disposable database,
	// preserving a real paid subscription, its ciphertext and business history.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0048_entitlement_epochs.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX ux_protocol_credential_subscription_endpoint ON protocol_credentials(subscription_id,protocol_endpoint_id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("protocol_credentials", "ux_credential_current_endpoint"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ table, column string }{{"protocol_credentials", "current_scope"}, {"protocol_credentials", "entitlement_epoch"}, {"subscriptions", "entitlement_epoch"}, {"traffic_records", "entitlement_epoch"}, {"quota_events", "entitlement_epoch"}} {
		if err := db.Exec("ALTER TABLE " + field.table + " DROP COLUMN " + field.column).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropTable("closed_entitlements"); err != nil {
		t.Fatal(err)
	}
	path := "0048_entitlement_epochs.up.sql"
	if datastore.IsSQLite(db) {
		path = "sqlite/0048_entitlement_epochs.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for _, statement := range strings.Split(string(payload), ";") {
		if strings.TrimSpace(statement) != "" {
			statements = append(statements, statement)
		}
	}
	count := len(statements)
	if mode == "partial_columns" {
		count = 2
	}
	if mode == "partial_index" {
		count = 7
	}
	for _, statement := range statements[:count] {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal("actual formal migration statement", err)
		}
	}
	if mode == "formal" {
		if err := db.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)", "0048_entitlement_epochs.up.sql", time.Now().UTC()).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("upgrade/repeated startup", err)
		}
	}
	var after model.ProtocolCredential
	if err := db.First(&after, original.ID).Error; err != nil || after.Secret != original.Secret || after.PrincipalKey != original.PrincipalKey || after.EntitlementEpoch != 0 || after.CurrentScope == nil || *after.CurrentScope != 1 {
		t.Fatal("upgrade lost/fabricated source identity", err)
	}
	var order model.Order
	if err := db.First(&order, paid.ID).Error; err != nil || order.PaidAmount != paid.PaidAmount || order.PurchaseSnapshot != paid.PurchaseSnapshot || order.EntitlementValueSnapshot != paid.EntitlementValueSnapshot {
		t.Fatal("upgrade rewrote historical sale", err)
	}
	if db.Migrator().HasIndex("protocol_credentials", "ux_protocol_credential_subscription_endpoint") || !db.Migrator().HasIndex("protocol_credentials", "ux_credential_current_endpoint") {
		t.Fatal("current/history scope constraint not replaced")
	}
	// Constraint remains strict for current ownership, while production recovery
	// tests exercise multiple historical rows retained under NULL scopes.
	duplicate := after
	duplicate.ID = 0
	duplicate.PrincipalKey = "duplicate-current-scope"
	duplicate.CredentialID = "duplicate-current-scope"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate current credential accepted")
	}
	if err := db.Exec("CREATE INDEX idx_credential_scope_repair_fixture ON protocol_credentials(subscription_id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex("protocol_credentials", "ux_credential_current_endpoint"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("derived unique index was not recoverable", err)
	}
	// Recorded source loss is never repaired by resetting epoch to zero.
	if err := db.Exec("ALTER TABLE protocol_credentials DROP COLUMN entitlement_epoch").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("missing epoch source silently reset")
	}
	if db.Migrator().HasColumn("protocol_credentials", "entitlement_epoch") {
		t.Fatal("preflight recreated lost attribution")
	}
}
