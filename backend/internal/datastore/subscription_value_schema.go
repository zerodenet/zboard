package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const subscriptionValueSchemaVersion = "0043_subscription_value_settlements.up.sql"

func preflightSubscriptionValueSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", subscriptionValueSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	tables := map[string][]string{
		"order_value_budgets":                   {"order_id", "user_id", "currency", "funding_minor", "refunded_minor", "converted_minor", "funding_fingerprint", "revision", "created_at", "updated_at"},
		"subscription_value_settlements":        {"id", "user_id", "subscription_id", "root_order_id", "request_key", "request_fingerprint", "currency", "kind", "amount_minor", "wallet_minor", "applied_minor", "entry_id", "actor_id", "reason", "quote_snapshot", "created_at"},
		"subscription_value_settlement_sources": {"settlement_id", "order_id", "amount_minor"},
	}
	complete := true
	for table, columns := range tables {
		if !db.Migrator().HasTable(table) {
			complete = false
			continue
		}
		// Fetch metadata once per table; startup must not issue one schema
		// query per financial field (especially across repeated worker starts).
		metadata, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return err
		}
		present := make(map[string]bool, len(metadata))
		for _, column := range metadata {
			present[column.Name()] = true
		}
		for _, column := range columns {
			if !present[column] {
				return fmt.Errorf("subscription value settlement financial column %s.%s missing; restore before startup", table, column)
			}
		}
	}
	if !complete {
		if applied > 0 {
			return fmt.Errorf("subscription value settlement financial tables missing after migration; restore settlement and cash-source evidence before startup")
		}
		for table := range tables {
			if db.Migrator().HasTable(table) {
				var row struct{ Found int }
				if err := db.Table(table).Select("1 AS found").Limit(1).Scan(&row).Error; err != nil {
					return err
				}
				if row.Found == 1 {
					return fmt.Errorf("incomplete subscription value settlement schema contains financial evidence; restore its counterpart")
				}
			}
		}
	}
	return nil
}
func reconcileSubscriptionValueSchema(db *gorm.DB) error {
	if err := preflightSubscriptionValueSchema(db); err != nil {
		return err
	}
	path := subscriptionValueSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0043_subscription_value_settlements.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	for i, table := range []string{"order_value_budgets", "subscription_value_settlements", "subscription_value_settlement_sources"} {
		if !db.Migrator().HasTable(table) {
			if err := createHistoricalTable(db, table, statements[i]); err != nil {
				return err
			}
		}
	}
	if IsSQLite(db) {
		for _, statement := range statements[3:] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	} else {
		for _, resource := range []struct {
			value              any
			relations, indexes []string
		}{
			{&model.OrderValueBudget{}, []string{"Order", "User"}, []string{"idx_order_value_budgets_user_id"}},
			{&model.SubscriptionValueSettlement{}, []string{"User", "Subscription", "RootOrder", "Entry", "Actor"}, []string{"idx_value_settlement_owner", "idx_subscription_value_settlements_root_order_id", "idx_subscription_value_settlements_request_key", "idx_subscription_value_settlements_entry_id"}},
			{&model.SubscriptionValueSettlementSource{}, []string{"Settlement", "Budget"}, []string{"idx_subscription_value_settlement_sources_order_id"}},
		} {
			for _, name := range resource.relations {
				if !db.Migrator().HasConstraint(resource.value, name) {
					if err := db.Migrator().CreateConstraint(resource.value, name); err != nil {
						return err
					}
				}
			}
			for _, name := range resource.indexes {
				if !db.Migrator().HasIndex(resource.value, name) {
					if err := db.Migrator().CreateIndex(resource.value, name); err != nil {
						return err
					}
				}
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: subscriptionValueSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
