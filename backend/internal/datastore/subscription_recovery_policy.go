package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const subscriptionRecoveryPolicyVersion = model.SubscriptionRecoveryPolicySchemaVersion

func preflightSubscriptionRecoveryPolicy(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", subscriptionRecoveryPolicyVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied == 0 {
		return nil
	}
	return validateSubscriptionRecoveryPolicy(db)
}
func validateSubscriptionRecoveryPolicy(db *gorm.DB) error {
	var row model.SystemConfig
	if err := db.Where("config_key = ?", entitlements.RecoveryGraceDaysKey).First(&row).Error; err != nil {
		return fmt.Errorf("initialized subscription recovery policy missing; restore before startup: %w", err)
	}
	if row.ValueType != "int" || row.IsSecret {
		return fmt.Errorf("subscription recovery policy metadata invalid; restore before startup")
	}
	if _, err := entitlements.ParseRecoveryGraceDays(row.Value); err != nil {
		return fmt.Errorf("subscription recovery policy invalid; restore before startup: %w", err)
	}
	return nil
}
func reconcileSubscriptionRecoveryPolicy(db *gorm.DB) error {
	if err := preflightSubscriptionRecoveryPolicy(db); err != nil {
		return err
	}
	path := subscriptionRecoveryPolicyVersion
	if IsSQLite(db) {
		path = "sqlite/0050_subscription_recovery_policy.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if err := db.Exec(statements[0]).Error; err != nil {
		return err
	}
	if err := validateSubscriptionRecoveryPolicy(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: subscriptionRecoveryPolicyVersion, AppliedAt: time.Now().UTC()}).Error
}
