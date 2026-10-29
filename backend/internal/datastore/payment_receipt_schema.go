package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const paymentReceiptSchemaVersion = "0039_payment_receipts.up.sql"

func preflightPaymentReceiptSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	tables := []string{"payment_attempts", "payment_receipts"}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", paymentReceiptSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	complete := true
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			complete = false
		}
	}
	if !complete {
		if applied > 0 {
			return fmt.Errorf("payment financial tables are missing after migration; restore and reconcile receipts before startup")
		}
		for _, table := range tables {
			if !db.Migrator().HasTable(table) {
				continue
			}
			var row struct{ Found int }
			if err := db.Table(table).Select("1 AS found").Limit(1).Scan(&row).Error; err != nil {
				return err
			}
			if row.Found == 1 {
				return fmt.Errorf("incomplete payment schema contains financial evidence; restore before startup")
			}
		}
	}
	if applied > 0 {
		for table, columns := range map[string][]string{
			"payment_attempts": {"order_id", "provider", "amount_minor", "currency"},
			"payment_receipts": {"receipt_key", "provider", "provider_trade_no", "order_id", "amount_minor", "currency", "disposition", "wallet_entry_id", "ticket_id", "resolution_reason", "resolved_by", "resolved_at"},
		} {
			for _, column := range columns {
				if !db.Migrator().HasColumn(table, column) {
					return fmt.Errorf("payment financial column %s.%s missing; restore before startup", table, column)
				}
			}
		}
	}
	return nil
}
func reconcilePaymentReceiptSchema(db *gorm.DB) error {
	if err := preflightPaymentReceiptSchema(db); err != nil {
		return err
	}
	path := paymentReceiptSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0039_payment_receipts.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	for i, table := range []string{"payment_attempts", "payment_receipts"} {
		if !db.Migrator().HasTable(table) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
	}
	if IsSQLite(db) {
		for _, stmt := range statements[2:] {
			if err := db.Exec(stmt).Error; err != nil {
				return err
			}
		}
	} else {
		for _, relation := range []string{"Order", "WalletEntry", "Ticket", "Resolver"} {
			if !db.Migrator().HasConstraint(&model.PaymentReceipt{}, relation) {
				if err := db.Migrator().CreateConstraint(&model.PaymentReceipt{}, relation); err != nil {
					return err
				}
			}
		}
		for _, item := range []struct {
			value any
			index string
		}{{&model.PaymentAttempt{}, "ux_payment_attempt"}, {&model.PaymentReceipt{}, "idx_payment_receipts_receipt_key"}} {
			if !db.Migrator().HasIndex(item.value, item.index) {
				if err := db.Migrator().CreateIndex(item.value, item.index); err != nil {
					return err
				}
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: paymentReceiptSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
