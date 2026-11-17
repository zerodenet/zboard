package datastore

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ticketReferenceSchemaVersion = "0058_ticket_references.up.sql"

var ticketReferenceColumns = []string{"order_id", "subscription_id", "wallet_entry_id"}

// Historical ticket context cannot be reconstructed from the current catalog.
// Check before SQLite AutoMigrate could silently replace missing evidence.
func preflightTicketReferenceSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", ticketReferenceSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	var present []string
	for _, column := range ticketReferenceColumns {
		if db.Migrator().HasColumn("tickets", column) {
			present = append(present, column)
		}
	}
	if len(present) == len(ticketReferenceColumns) {
		return nil
	}
	if applied != 0 {
		return fmt.Errorf("ticket reference evidence missing; restore before startup")
	}
	if len(present) != 0 {
		conditions := make([]string, 0, len(present))
		for _, column := range present {
			conditions = append(conditions, column+" IS NOT NULL")
		}
		var evidence struct{ Present int }
		if err := db.Table("tickets").Select("1 AS present").Where(strings.Join(conditions, " OR ")).Limit(1).Find(&evidence).Error; err != nil {
			return err
		}
		if evidence.Present != 0 {
			return fmt.Errorf("incomplete ticket reference schema contains evidence; restore before startup")
		}
	}
	return nil
}

func reconcileTicketReferenceSchema(db *gorm.DB) error {
	if err := preflightTicketReferenceSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable("tickets") {
		return fmt.Errorf("ticket table missing; restore before startup")
	}
	var additions []string
	for _, column := range ticketReferenceColumns {
		if db.Migrator().HasColumn("tickets", column) {
			continue
		}
		definition := "bigint unsigned NULL DEFAULT NULL"
		if IsSQLite(db) {
			definition = "integer NULL DEFAULT NULL"
		}
		additions = append(additions, "ADD COLUMN "+column+" "+definition)
	}
	if IsSQLite(db) {
		for _, addition := range additions {
			if err := db.Exec("ALTER TABLE tickets " + addition).Error; err != nil {
				return err
			}
		}
	} else if len(additions) != 0 {
		if err := db.Exec("ALTER TABLE tickets " + strings.Join(additions, ", ")).Error; err != nil {
			return err
		}
	}
	if err := preflightTicketReferenceSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: ticketReferenceSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
