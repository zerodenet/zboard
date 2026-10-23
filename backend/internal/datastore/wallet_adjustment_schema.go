package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const walletAdjustmentSchemaVersion = "0041_wallet_adjustments.up.sql"

func preflightWalletAdjustmentSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", walletAdjustmentSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	tables := map[string][]string{
		"wallet_adjustments":    {"id", "request_key", "fingerprint", "user_id", "currency", "amount_minor", "reason", "ticket_id", "actor_id", "entry_id", "reversal_entry_id", "reversal_actor_id", "reversal_reason", "external_status", "created_at", "updated_at"},
		"wallet_payout_results": {"id", "adjustment_id", "request_key", "fingerprint", "actor_id", "outcome", "reference", "note", "occurred_at", "created_at"},
	}
	complete := true
	for table, columns := range tables {
		if !db.Migrator().HasTable(table) {
			complete = false
			continue
		}
		for _, column := range columns {
			if !db.Migrator().HasColumn(table, column) {
				return fmt.Errorf("wallet adjustment financial column %s.%s missing; restore before startup", table, column)
			}
		}
	}
	if !complete {
		if applied > 0 {
			return fmt.Errorf("wallet adjustment financial tables missing after migration; restore debit and payout evidence before startup")
		}
		for table := range tables {
			if db.Migrator().HasTable(table) {
				var row struct{ Found int }
				if err := db.Table(table).Select("1 AS found").Limit(1).Scan(&row).Error; err != nil {
					return err
				}
				if row.Found == 1 {
					return fmt.Errorf("incomplete wallet adjustment schema contains financial evidence; restore its counterpart")
				}
			}
		}
	}
	return nil
}
func reconcileWalletAdjustmentSchema(db *gorm.DB) error {
	if err := preflightWalletAdjustmentSchema(db); err != nil {
		return err
	}
	path := walletAdjustmentSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0041_wallet_adjustments.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	for i, table := range []string{"wallet_adjustments", "wallet_payout_results"} {
		if !db.Migrator().HasTable(table) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
	}
	if IsSQLite(db) {
		for _, statement := range statements[2:] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	} else {
		for _, resource := range []struct {
			value              any
			relations, indexes []string
		}{
			{&model.WalletAdjustment{}, []string{"User", "Actor", "ReversalActor", "Ticket", "Entry", "ReversalEntry"}, []string{"idx_wallet_adjustments_request_key", "idx_wallet_adjustments_entry_id", "idx_wallet_adjustments_reversal_entry_id"}},
			{&model.WalletPayoutResult{}, []string{"Adjustment", "Actor"}, []string{"idx_wallet_payout_results_request_key"}},
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
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: walletAdjustmentSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
