package datastore

import (
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

// This derived table is rebuilt by ledger inserts during database transfers;
// it is deliberately excluded from the logical business table copy inventory.
func reconcileSQLiteTrafficHourly(db *gorm.DB) error {
	const version = "0024_traffic_usage_hourly.up.sql"
	var count int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	payload, err := migrations.Files.ReadFile("sqlite/0024_traffic_usage_hourly.sql")
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		for _, statement := range sqliteTrafficHourlyTriggers() {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return tx.Create(&schemaMigration{Version: version, AppliedAt: time.Now().UTC()}).Error
	})
}

func sqliteTrafficHourlyTriggers() []string {
	dimensions := []string{"user_id", "subscription_id", "node_id", "protocol_endpoint_id", "protocol_multiplier_milli"}
	values := []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"}
	columns := append(append([]string{"record_at"}, dimensions...), values...)
	newValues := []string{"strftime('%Y-%m-%d %H:00:00', NEW.record_at)"}
	oldWhere := []string{"record_at = strftime('%Y-%m-%d %H:00:00', OLD.record_at)"}
	for _, column := range dimensions {
		newValues = append(newValues, "COALESCE(NEW."+column+", 0)")
		oldWhere = append(oldWhere, column+" = COALESCE(OLD."+column+", 0)")
	}
	var additions, subtractions []string
	for _, column := range values {
		value := "COALESCE(NEW." + column + ", 0)"
		old := "COALESCE(OLD." + column + ", 0)"
		if column == "record_count" {
			value, old = "1", "1"
		}
		newValues = append(newValues, value)
		additions = append(additions, column+" = "+column+" + excluded."+column)
		subtractions = append(subtractions, column+" = "+column+" - "+old)
	}
	insert := "INSERT INTO traffic_usage_hourly(" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(newValues, ", ") + ") ON CONFLICT DO UPDATE SET " + strings.Join(additions, ", ")
	subtract := "UPDATE traffic_usage_hourly SET " + strings.Join(subtractions, ", ") + " WHERE " + strings.Join(oldWhere, " AND ")
	// REPLACE suppresses implicit DELETE triggers when recursive_triggers is off.
	// Subtract its previous row before insertion in that mode only.
	var replacementWhere, replacementChanges []string
	replacementWhere = append(replacementWhere, "record_at = (SELECT strftime('%Y-%m-%d %H:00:00', record_at) FROM traffic_records WHERE id = NEW.id)")
	for _, column := range dimensions {
		replacementWhere = append(replacementWhere, column+" = (SELECT COALESCE("+column+", 0) FROM traffic_records WHERE id = NEW.id)")
	}
	for _, column := range values {
		old := "(SELECT COALESCE(" + column + ", 0) FROM traffic_records WHERE id = NEW.id)"
		if column == "record_count" {
			old = "1"
		}
		replacementChanges = append(replacementChanges, column+" = "+column+" - "+old)
	}
	return []string{
		"CREATE TRIGGER traffic_hourly_replace BEFORE INSERT ON traffic_records WHEN EXISTS(SELECT 1 FROM traffic_records WHERE id = NEW.id) AND (SELECT recursive_triggers FROM pragma_recursive_triggers) = 0 BEGIN UPDATE traffic_usage_hourly SET " + strings.Join(replacementChanges, ", ") + " WHERE " + strings.Join(replacementWhere, " AND ") + "; END",
		"CREATE TRIGGER traffic_hourly_insert AFTER INSERT ON traffic_records BEGIN " + insert + "; END",
		"CREATE TRIGGER traffic_hourly_update AFTER UPDATE ON traffic_records BEGIN " + subtract + "; " + insert + "; END",
		"CREATE TRIGGER traffic_hourly_delete AFTER DELETE ON traffic_records BEGIN " + subtract + "; END",
	}
}
