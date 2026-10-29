package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const consumptionRefundSchemaVersion = "0052_consumption_refunds.up.sql"

func preflightConsumptionRefundSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if db.Migrator().HasTable("consumption_refunds") {
		columns, err := db.Migrator().ColumnTypes("consumption_refunds")
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for _, column := range columns {
			present[column.Name()] = true
		}
		for _, column := range []string{"id", "request_key", "fingerprint", "order_id", "user_id", "currency", "amount_minor", "ticket_id", "actor_id", "reason", "entry_id", "created_at"} {
			if !present[column] {
				return fmt.Errorf("consumption refund financial column consumption_refunds.%s missing; restore before startup", column)
			}
		}
		return nil
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", consumptionRefundSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if applied > 0 {
		return fmt.Errorf("consumption refund financial table missing after migration; restore approval and idempotency evidence before startup")
	}
	// An interrupted migration may lack its marker. Surviving wallet/audit
	// records still forbid replacing a lost approval journal with an empty one.
	for _, source := range []struct{ table, where, value string }{
		{"wallet_entries", "kind = ?", "consumption_refund"},
		{"audit_logs", "action = ?", "order.consumption_refund"},
	} {
		if !db.Migrator().HasTable(source.table) {
			continue
		}
		var found struct{ ID uint }
		if err := db.Table(source.table).Select("id").Where(source.where, source.value).Limit(1).Scan(&found).Error; err != nil {
			return err
		}
		if found.ID != 0 {
			return fmt.Errorf("consumption refund evidence survives without approval journal; restore before startup")
		}
	}
	return nil
}

func reconcileConsumptionRefundSchema(db *gorm.DB) error {
	if err := preflightConsumptionRefundSchema(db); err != nil {
		return err
	}
	path := consumptionRefundSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0052_consumption_refunds.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if !db.Migrator().HasTable("consumption_refunds") {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if IsSQLite(db) {
		for _, statement := range statements[1:] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	} else {
		for _, relation := range []string{"Order", "Budget", "User", "Actor", "Ticket", "Entry"} {
			if !db.Migrator().HasConstraint(&model.ConsumptionRefund{}, relation) {
				if err := db.Migrator().CreateConstraint(&model.ConsumptionRefund{}, relation); err != nil {
					return err
				}
			}
		}
		for _, index := range []string{"idx_consumption_refunds_request_key", "idx_consumption_refunds_entry_id", "idx_consumption_refund_order", "idx_consumption_refunds_user_id", "idx_consumption_refunds_ticket_id", "idx_consumption_refunds_actor_id"} {
			if !db.Migrator().HasIndex(&model.ConsumptionRefund{}, index) {
				if err := db.Migrator().CreateIndex(&model.ConsumptionRefund{}, index); err != nil {
					return err
				}
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: consumptionRefundSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
