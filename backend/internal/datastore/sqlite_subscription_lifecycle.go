package datastore

import (
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"time"
)

func reconcileSQLiteSubscriptionLifecycle(db *gorm.DB) error {
	const version = "0023_subscription_lifecycle.up.sql"
	var count int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	payload, err := migrations.Files.ReadFile("sqlite/0023_subscription_lifecycle.sql")
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	columns := []struct{ table, name string }{
		{"subscriptions", "lifecycle"}, {"subscriptions", "ends_on_quota_exhaustion"}, {"subscriptions", "ended_at"}, {"subscriptions", "end_reason"},
		{"orders", "subscription_ended_at"}, {"orders", "subscription_end_reason"}, {"orders", "subscription_final_flow_total"}, {"orders", "subscription_final_flow_used"},
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for index, column := range columns {
			if !tx.Migrator().HasColumn(column.table, column.name) {
				if err := tx.Exec(statements[index]).Error; err != nil {
					return err
				}
			}
		}
		for _, statement := range statements[len(columns):] {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return tx.Create(&schemaMigration{Version: version, AppliedAt: time.Now().UTC()}).Error
	})
}
