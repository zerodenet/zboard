package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const subscriptionRetirementRetryVersion = "0072_subscription_retirement_retry.up.sql"

// This schedule is not a money source. An interrupted/recorded installation
// can restore a missing nullable column to immediate eligibility; no order,
// wallet, quota, idempotency or audit evidence is inferred or rewritten.
func reconcileSubscriptionRetirementRetrySchema(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("subscriptions") {
		return fmt.Errorf("retirement retry preparation requires subscriptions")
	}
	asset := subscriptionRetirementRetryVersion
	if IsSQLite(db) {
		asset = "sqlite/0072_subscription_retirement_retry.sql"
	}
	payload, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if len(statements) != 2 {
		return fmt.Errorf("retirement retry migration must contain its column and index")
	}
	columns, _, err := projectionColumns(db, "subscriptions")
	if err != nil {
		return fmt.Errorf("inspect retirement retry installation: %w", err)
	}
	if _, present := columns["retirement_retry_at"]; !present {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if err := validateSubscriptionRetirementRetryColumn(db); err != nil {
		return err
	}
	if !db.Migrator().HasIndex("subscriptions", "idx_subscriptions_retirement_retry") {
		if err := db.Exec(statements[1]).Error; err != nil {
			return err
		}
	}
	indexes, err := projectionTableIndexes(db, "subscriptions")
	if err != nil {
		return fmt.Errorf("inspect retirement retry index: %w", err)
	}
	found := false
	for _, index := range indexes {
		found = found || index == (projectionIndex{columns: "retirement_retry_at,id", unique: false})
	}
	if !found {
		return fmt.Errorf("subscription retirement retry index shape is not prepared")
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: subscriptionRetirementRetryVersion, AppliedAt: time.Now().UTC()}).Error
}

// Metadata-only validation: copying or inspecting a database never repairs it.
// Projection workers do not run lifecycle retirement and do not require0072.
func validateSubscriptionRetirementRetryColumn(db *gorm.DB) error {
	columns, _, err := projectionColumns(db, "subscriptions")
	if err != nil {
		return fmt.Errorf("inspect retirement retry column: %w", err)
	}
	column, ok := columns["retirement_retry_at"]
	want := "datetime(3)"
	if IsSQLite(db) {
		want = "datetime"
	}
	if !ok || column.kind != want || !column.nullable {
		return fmt.Errorf("subscription retirement retry column is not prepared; run owned schema preparation")
	}
	return nil
}
