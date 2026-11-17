package datastore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteAutomaticRecurringApprovalMigrationPreservesAuthority(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertAutomaticRecurringApprovalMigration(t, db)
}
func TestMySQLAutomaticRecurringApprovalMigrationPreservesAuthority(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertAutomaticRecurringApprovalMigration(t, db)
}

func assertAutomaticRecurringApprovalMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	const receipt = "0057_automatic_recurring_approval.up.sql"
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	user := model.User{Email: "recurring-schema@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "recurring-schema", Code: "recurring-schema", IsEnabled: true}
	for _, row := range []any{&user, &group} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "recurring-schema", Slug: "recurring-schema", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "recurring-schema", Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", IsActive: true}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, StartAt: now, EndAt: now.AddDate(0, 1, 0), Status: "active", Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	root := model.Order{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, SubscriptionID: sub.ID, TradeNo: "recurring-schema", Status: "paid", Currency: "CNY"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	row := model.SubscriptionAutomaticRenewal{SubscriptionID: sub.ID, UserID: user.ID, Enabled: true, PlanSKUID: sku.ID, RootOrderID: root.ID, AgreementDigest: strings.Repeat("a", 64), AgreementVersion: 1, Revision: 7, State: "waiting", NextAttemptAt: sub.EndAt}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// The first ALTER may have committed before a crash. Existing V1 consent
	// remains V1; completing DDL never invents a recurring approval.
	for _, columns := range [][]string{{"approval_snapshot"}, {"approval_snapshot", "agreement_version"}} {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			if err := db.Migrator().DropColumn("subscription_automatic_renewals", column); err != nil {
				t.Fatal(err)
			}
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("interrupted/legacy DDL recovery", err)
		}
		var saved model.SubscriptionAutomaticRenewal
		if err := db.First(&saved, "subscription_id = ?", sub.ID).Error; err != nil || saved.Revision != 7 || saved.AgreementVersion != 1 || saved.ApprovalSnapshot != "" || saved.AgreementDigest != row.AgreementDigest || !saved.Enabled {
			t.Fatal("legacy approval rewritten", err)
		}
	}
	// Schema preservation fixture, not a claim of valid checkout authority.
	// The repository independently strictly validates all approval fields.
	raw := `{"version":2,"opaque_preservation_fixture":"` + strings.Repeat("已批准条款", 600) + `"}`
	hash := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(hash[:])
	if err := db.Model(&row).Updates(map[string]any{"agreement_version": 2, "approval_snapshot": raw, "agreement_digest": digest}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("DDL before receipt", err)
		}
		var saved model.SubscriptionAutomaticRenewal
		if err := db.First(&saved, "subscription_id = ?", sub.ID).Error; err != nil || saved.ApprovalSnapshot != raw || saved.AgreementDigest != digest || saved.Revision != 7 {
			t.Fatal("approval bytes changed", err)
		}
	}
	down, err := migrations.Files.ReadFile("0057_automatic_recurring_approval.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(down)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("approval_snapshot", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("one damaged approval blocked the complete application schema", err)
	}
	var damaged model.SubscriptionAutomaticRenewal
	if err := db.First(&damaged, "subscription_id = ?", sub.ID).Error; err != nil || damaged.ApprovalSnapshot != "" || damaged.AgreementVersion != 2 || damaged.AgreementDigest != digest {
		t.Fatal("startup rewrote damaged per-item authority", err)
	}
	if err := db.Model(&row).Update("approval_snapshot", raw).Error; err != nil {
		t.Fatal(err)
	}
	// A missing receipt does not make an existing half of V2 authority safe
	// to default. Check both crash/loss directions before SQLite AutoMigrate.
	for _, column := range []string{"agreement_version", "approval_snapshot"} {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn("subscription_automatic_renewals", column); err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "contains authority") {
			t.Fatal("partial V2 authority without receipt was defaulted", column, err)
		}
		if db.Migrator().HasColumn("subscription_automatic_renewals", column) {
			t.Fatal("partial authority was repaired before operator restoration", column)
		}
		ddl := "ALTER TABLE subscription_automatic_renewals ADD COLUMN agreement_version int NOT NULL DEFAULT 1"
		if column == "approval_snapshot" {
			ddl = "ALTER TABLE subscription_automatic_renewals ADD COLUMN approval_snapshot TEXT NOT NULL DEFAULT ('')"
		}
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&row).Updates(map[string]any{"agreement_version": 2, "approval_snapshot": raw}).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("restored authority", err)
		}
	}
	if err := db.Migrator().DropColumn("subscription_automatic_renewals", "approval_snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "approval authority missing") {
		t.Fatal("recorded lost authority column repaired", err)
	}
	if db.Migrator().HasColumn("subscription_automatic_renewals", "approval_snapshot") {
		t.Fatal("SQLite AutoMigrate ran before authority preflight")
	}
}
