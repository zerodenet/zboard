package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const automaticRenewalSchemaVersion = "0051_subscription_automatic_renewals.up.sql"

func preflightAutomaticRenewalSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", automaticRenewalSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	complete := true
	for table, columns := range map[string][]string{
		"subscription_automatic_renewals": {"subscription_id", "user_id", "enabled", "plan_sku_id", "root_order_id", "agreement_digest", "revision", "state", "message", "last_order_id", "last_attempt_at", "attempted_end_at", "attempt_sequence", "next_attempt_at", "created_at", "updated_at"},
		"automatic_renewal_scans":         {"id", "sequence"},
	} {
		if !db.Migrator().HasTable(table) {
			complete = false
			continue
		}
		metadata, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for _, column := range metadata {
			present[column.Name()] = true
		}
		for _, column := range columns {
			if !present[column] {
				return fmt.Errorf("automatic renewal authority column %s.%s missing; restore before startup", table, column)
			}
		}
	}
	if applied > 0 {
		if !complete {
			return fmt.Errorf("automatic renewal consent or ordering source missing; restore before startup")
		}
		var guard model.AutomaticRenewalScan
		if err := db.First(&guard, 1).Error; err != nil {
			return fmt.Errorf("automatic renewal ordering source missing; restore before startup: %w", err)
		}
	}
	if applied == 0 && db.Migrator().HasTable("subscription_automatic_renewals") {
		var count int64
		if err := db.Model(&model.SubscriptionAutomaticRenewal{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			if !complete {
				return fmt.Errorf("incomplete automatic renewal schema contains consent; restore counterpart before startup")
			}
			var guard model.AutomaticRenewalScan
			if err := db.First(&guard, 1).Error; err != nil {
				return fmt.Errorf("automatic renewal consent exists without ordering source; restore before startup: %w", err)
			}
		}
	}
	return nil
}

func reconcileAutomaticRenewalSchema(db *gorm.DB) error {
	if err := preflightAutomaticRenewalSchema(db); err != nil {
		return err
	}
	path := automaticRenewalSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0051_subscription_automatic_renewals.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	for i, table := range []string{"subscription_automatic_renewals", "automatic_renewal_scans"} {
		if !db.Migrator().HasTable(table) {
			if err := createHistoricalTable(db, table, statements[i]); err != nil {
				return err
			}
		}
	}
	for _, statement := range statements[2:] {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	if !IsSQLite(db) {
		for _, name := range []string{"Subscription", "User", "SKU", "RootOrder", "LastOrder"} {
			if !db.Migrator().HasConstraint(&model.SubscriptionAutomaticRenewal{}, name) {
				if err := db.Migrator().CreateConstraint(&model.SubscriptionAutomaticRenewal{}, name); err != nil {
					return err
				}
			}
		}
		if !db.Migrator().HasIndex(&model.SubscriptionAutomaticRenewal{}, "idx_automatic_renewal_due") {
			if err := db.Migrator().CreateIndex(&model.SubscriptionAutomaticRenewal{}, "idx_automatic_renewal_due"); err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: automaticRenewalSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
