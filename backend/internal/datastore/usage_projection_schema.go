package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const usageProjectionSchemaVersion = "0053_metering_usage_projection_pending.up.sql"
const usageProjectionTable = "metering_usage_projection_pending"

func usageProjectionColumns(db *gorm.DB) (map[string]bool, error) {
	columns, err := db.Migrator().ColumnTypes(usageProjectionTable)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(columns))
	for _, column := range columns {
		present[column.Name()] = true
	}
	return present, nil
}

// Run before SQLite AutoMigrate. A missing reliable queue after cutover is data
// loss, even though an empty replacement table could satisfy the SQL schema.
func preflightUsageProjectionSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", usageProjectionSchemaVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasTable(usageProjectionTable) {
		if applied > 0 {
			return fmt.Errorf("metering usage projection recovery queue missing; restore before startup")
		}
		return nil
	}
	present, err := usageProjectionColumns(db)
	if err != nil {
		return err
	}
	for _, column := range []string{"traffic_record_id", "protocol_endpoint_id", "created_at"} {
		if present[column] {
			continue
		}
		var rows int64
		if err := db.Table(usageProjectionTable).Count(&rows).Error; err != nil {
			return err
		}
		if applied > 0 || rows > 0 {
			return fmt.Errorf("metering usage projection recovery column %s missing; restore before startup", column)
		}
	}
	if IsSQLite(db) && present["traffic_record_id"] && present["protocol_endpoint_id"] && present["created_at"] {
		var constraints []struct {
			Table    string `gorm:"column:table"`
			From     string `gorm:"column:from"`
			To       string `gorm:"column:to"`
			OnDelete string `gorm:"column:on_delete"`
		}
		if err := db.Raw("PRAGMA foreign_key_list('metering_usage_projection_pending')").Scan(&constraints).Error; err != nil {
			return err
		}
		protected := false
		for _, constraint := range constraints {
			if constraint.Table == "traffic_records" && constraint.From == "traffic_record_id" && constraint.To == "id" && constraint.OnDelete == "RESTRICT" {
				protected = true
			}
		}
		if !protected {
			return fmt.Errorf("metering usage projection source protection missing; restore foreign key before startup")
		}
	}
	return nil
}

func reconcileUsageProjectionSchema(db *gorm.DB) error {
	if err := preflightUsageProjectionSchema(db); err != nil {
		return err
	}
	path := usageProjectionSchemaVersion
	if IsSQLite(db) {
		path = "sqlite/0053_metering_usage_projection_pending.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	if db.Migrator().HasTable(usageProjectionTable) {
		present, err := usageProjectionColumns(db)
		if err != nil {
			return err
		}
		complete := present["traffic_record_id"] && present["protocol_endpoint_id"] && present["created_at"]
		if !complete {
			// preflight permits this only before cutover and with no queue rows.
			// Recreate from official DDL, never fabricate missing intent identity.
			if err := db.Migrator().DropTable(usageProjectionTable); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasTable(usageProjectionTable) {
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(usageProjectionTable, "idx_usage_projection_endpoint") {
		if err := db.Exec("CREATE INDEX idx_usage_projection_endpoint ON " + usageProjectionTable + "(protocol_endpoint_id,traffic_record_id)").Error; err != nil {
			return err
		}
	}
	if !IsSQLite(db) && !db.Migrator().HasConstraint(usageProjectionTable, "fk_usage_projection_traffic") {
		if err := db.Exec("ALTER TABLE " + usageProjectionTable + " ADD CONSTRAINT fk_usage_projection_traffic FOREIGN KEY(traffic_record_id) REFERENCES traffic_records(id) ON DELETE RESTRICT").Error; err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: usageProjectionSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
