package datastore_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const retirementRetryMigration = "0072_subscription_retirement_retry.up.sql"

func TestSQLiteRetirementRetryUpgradeRecoveryAndLegacyCopy(t *testing.T) {
	retirementRetryUpgrade(t, false)
}

func TestMySQLRetirementRetryUpgradeRecoveryAndLegacyCopy(t *testing.T) {
	retirementRetryUpgrade(t, true)
}

func retirementRetryUpgrade(t *testing.T, mysql bool) {
	t.Helper()
	db := diagnosticSchemaDB(t, mysql)
	user := model.User{Email: "retirement-schema@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "Retirement schema", Code: "retirement-schema", IsEnabled: true}
	for _, row := range []any{&user, &group} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "Retirement schema", Slug: "retirement-schema", NodeGroupID: group.ID}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "retirement-schema", Name: "Retirement schema", Currency: "CNY", BillingUnit: "month", BillingValue: 1, BillingMode: "periodic", EntitlementMode: "plan", RenewalEffect: "extend_only"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	retry := at.Add(30 * time.Minute)
	next := at.AddDate(0, 1, 0)
	// Metadata-only rights are sufficient for schema preservation. This test
	// never invents a payment, refundable value, funding identity or paid order.
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "active", StartAt: at, EndAt: next, NextResetAt: &next, FlowTotal: 100, FlowUsed: 60, ResetQuotaBytes: 100, Config: "{}", RetirementRetryAt: &retry}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	before := sub
	assertSame := func(want model.Subscription) {
		t.Helper()
		var actual model.Subscription
		if err := db.First(&actual, sub.ID).Error; err != nil || !reflect.DeepEqual(actual, want) {
			t.Fatal("schema preparation changed rights or scheduling", actual, want, err)
		}
		var orders int64
		if err := db.Model(&model.Order{}).Count(&orders).Error; err != nil || orders != 0 {
			t.Fatal("schema preparation fabricated financial history", orders, err)
		}
	}
	// Installation may stop after both DDL statements but before its receipt.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", retirementRetryMigration).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("repeat interrupted installation", err)
		}
		assertSame(before)
	}
	// A missing index is repaired without moving an existing retry clock.
	if err := db.Migrator().DropIndex("subscriptions", "idx_subscriptions_retirement_retry"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	assertSame(before)
	removeColumn := func(database *gorm.DB) {
		t.Helper()
		if err := database.Migrator().DropIndex("subscriptions", "idx_subscriptions_retirement_retry"); err != nil {
			t.Fatal(err)
		}
		if err := database.Exec("ALTER TABLE subscriptions DROP COLUMN retirement_retry_at").Error; err != nil {
			t.Fatal(err)
		}
	}
	removeColumn(db)
	// The target is actually prepared and exclusively owned. A source with a
	// lost operational column remains readable and imports immediate eligibility.
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var copied model.Subscription
	if err := target.First(&copied, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	want := before
	want.RetirementRetryAt = nil
	if !reflect.DeepEqual(copied, want) || db.Migrator().HasColumn("subscriptions", "retirement_retry_at") {
		t.Fatal("legacy copy changed rights or repaired its source", copied, want)
	}
	// Owned preparation can restore this non-financial column even when0072
	// remains recorded. NULL only re-enables retry; it does not mark completion.
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("recorded missing-column recovery", err)
		}
		assertSame(want)
	}
	// Copy admission must reject an unprepared target, never perform its DDL.
	removeColumn(target)
	err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(db, tx)
	})
	if err == nil || !strings.Contains(err.Error(), "retirement retry column is not prepared") || target.Migrator().HasColumn("subscriptions", "retirement_retry_at") {
		t.Fatal("copy silently prepared or admitted missing retry column", err)
	}
}
