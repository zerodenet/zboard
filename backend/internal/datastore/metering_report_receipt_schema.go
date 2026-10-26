package datastore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const reportReceiptVersion = "0063_metering_report_receipts.up.sql"
const reportReceiptTable = "metering_report_receipts"

func reportIdentityCollation(db *gorm.DB) (string, error) {
	if IsSQLite(db) {
		var rows []struct{ Coll string }
		if err := db.Raw("SELECT coll FROM pragma_index_xinfo(?) WHERE name = ? AND key = 1", "ux_traffic_node_report", "report_id").Scan(&rows).Error; err != nil {
			return "", err
		}
		if len(rows) != 1 || !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(rows[0].Coll) {
			return "", errors.New("source report identity collation unavailable")
		}
		return rows[0].Coll, nil
	}
	columns, _, err := projectionColumns(db, "traffic_records")
	if err != nil {
		return "", err
	}
	c, ok := columns["report_id"]
	if !ok || c.kind != "varchar(191)" || !strings.HasPrefix(c.collation, "utf8mb4_") || !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(c.collation) {
		return "", errors.New("unsupported source report identity collation")
	}
	return c.collation, nil
}

// An installed receipt table is accounting recovery authority. Never recreate
// a recorded lost table or silently adopt changed keys/collations as empty.
func preflightReportReceiptSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", reportReceiptVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if !tables[reportReceiptTable] {
		if applied > 0 {
			return errors.New("recorded metering report identities missing; restore before startup/copy")
		}
		return nil
	}
	columns, primary, err := projectionColumns(db, reportReceiptTable)
	if err != nil {
		return err
	}
	if len(columns) != 5 || strings.Join(primary, ",") != "node_id,report_id" {
		return errors.New("metering report identity columns or primary key differ")
	}
	for _, name := range []string{"node_id", "report_id", "protocol_endpoint_id", "traffic_record_id", "created_at"} {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		if name == "report_id" {
			kind = "varchar(191)"
			if IsSQLite(db) {
				kind = "text"
			}
		}
		if name == "created_at" {
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		column, ok := columns[name]
		if !ok || column.kind != kind || column.nullable {
			return fmt.Errorf("metering report identity column %s differs", name)
		}
	}
	if !tables["traffic_records"] {
		var row struct{ TrafficRecordID uint }
		found := db.Table(reportReceiptTable).Select("traffic_record_id").Limit(1).Scan(&row)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			return errors.New("metering receipt source table missing; restore source schema before startup/copy")
		}
		// Preserve baseline's empty partial-table recovery. The completed preparation
		// below must still validate physical source equality before opening work.
		return nil
	}
	want, err := reportIdentityCollation(db)
	if err != nil {
		return err
	}
	if IsSQLite(db) {
		var refs []struct {
			Table string `gorm:"column:table"`
		}
		if err := db.Raw("PRAGMA foreign_key_list(metering_report_receipts)").Scan(&refs).Error; err != nil {
			return err
		}
		if len(refs) != 0 {
			return errors.New("metering report identities must not depend on fact foreign keys")
		}
		var actual []struct{ Coll string }
		if err := db.Raw("SELECT x.coll FROM pragma_index_list(?) i JOIN pragma_index_xinfo(i.name) x WHERE i.origin = 'pk' AND x.name = ? AND x.key = 1", reportReceiptTable, "report_id").Scan(&actual).Error; err != nil {
			return err
		}
		if len(actual) != 1 || !strings.EqualFold(actual[0].Coll, want) {
			return errors.New("metering report identity collation differs from ledger")
		}
	} else {
		if columns["report_id"].collation != want {
			return errors.New("metering report identity collation differs from ledger")
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL", reportReceiptTable).Scan(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("metering report identities must not depend on fact foreign keys")
		}
	}
	return nil
}

func reconcileReportReceiptSchema(db *gorm.DB) error {
	if err := preflightReportReceiptSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable(reportReceiptTable) {
		asset := reportReceiptVersion
		if IsSQLite(db) {
			asset = "sqlite/0063_metering_report_receipts.sql"
		}
		raw, err := migrations.Files.ReadFile(asset)
		if err != nil {
			return err
		}
		collation, err := reportIdentityCollation(db)
		if err != nil {
			return err
		}
		source := string(raw)
		// Inherit the predecessor's actual equality semantics, including a valid
		// custom MySQL collation; never rewrite existing financial report IDs.
		if IsSQLite(db) {
			source = strings.Replace(source, "COLLATE BINARY", "COLLATE "+collation, 1)
		} else {
			source = strings.Replace(source, "COLLATE utf8mb4_0900_ai_ci", "COLLATE "+collation, 1)
		}
		statements, err := splitMigrationStatements(source)
		if err != nil || len(statements) != 1 {
			return errors.New("unexpected metering report receipt migration inventory")
		}
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if err := preflightReportReceiptSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: reportReceiptVersion, AppliedAt: time.Now().UTC()}).Error
}
