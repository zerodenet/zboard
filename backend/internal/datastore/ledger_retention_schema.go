package datastore

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ledgerRetentionVersion = "0087_metering_ledger_retention.up.sql"

func nonceIdentityCollation(db *gorm.DB, table, column string) (string, error) {
	var c string
	if IsSQLite(db) {
		var cs []string
		if err := db.Raw("SELECT x.coll FROM pragma_index_list(?) i JOIN pragma_index_xinfo(i.name) x WHERE i.[unique]=1 AND x.name=? AND x.key=1", table, column).Scan(&cs).Error; err != nil {
			return "", err
		}
		if len(cs) != 1 {
			return "", errors.New("nonce identity index missing")
		}
		c = cs[0]
	} else {
		if err := db.Raw("SELECT COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?", table, column).Scan(&c).Error; err != nil {
			return "", err
		}
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(c) {
		return "", errors.New("nonce identity collation missing")
	}
	return c, nil
}
func validateLedgerRetentionSchema(db *gorm.DB) error {
	for _, spec := range []struct {
		table       string
		columns, pk []string
	}{
		{"metering_ledger_targets", []string{"source_id", "node_id", "protocol_endpoint_id", "record_id", "result_id"}, []string{"source_id"}},
		{"metering_nonce_receipts", []string{"node_id", "nonce", "report_id"}, []string{"node_id", "nonce"}},
		{"metering_ledger_retention_progress", []string{"high_water_id", "held_count", "last_held_id", "last_hold_reason", "id", "after_id", "upper_id", "next_pass_at"}, []string{"id"}},
	} {
		cols, pk, err := projectionColumns(db, spec.table)
		if err != nil {
			return err
		}
		if len(cols) != len(spec.columns) || !slices.Equal(pk, spec.pk) {
			return errors.New("ledger retention shape differs: " + spec.table)
		}
		for _, name := range spec.columns {
			c, ok := cols[name]
			kind := "bigint unsigned"
			if name == "last_hold_reason" {
				kind = "varchar(255)"
			}
			if name == "nonce" {
				kind = "varchar(64)"
			}
			if name == "report_id" {
				kind = "varchar(191)"
			}
			if name == "next_pass_at" {
				kind = "datetime(3)"
			}
			if IsSQLite(db) {
				kind = "integer"
				if name == "last_hold_reason" || name == "nonce" || name == "report_id" {
					kind = "text"
				}
				if name == "next_pass_at" {
					kind = "datetime"
				}
			}
			if !ok || c.nullable || c.kind != kind {
				return errors.New("ledger retention column differs: " + name)
			}
		}
	}
	for _, spec := range []struct{ table, col string }{{"traffic_records", "retention_level"}} {
		cs, _, err := projectionColumns(db, spec.table)
		if err != nil {
			return err
		}
		c, ok := cs[spec.col]
		kind := "smallint"
		if IsSQLite(db) {
			kind = "integer"
		}
		if !ok || c.nullable || c.kind != kind || strings.Trim(c.defaultSQL, "()\"' ") != "0" {
			return errors.New("ledger retention column missing: " + spec.col)
		}
	}
	want, err := nonceIdentityCollation(db, "traffic_records", "nonce")
	if err != nil {
		return err
	}
	got, err := nonceIdentityCollation(db, "metering_nonce_receipts", "nonce")
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, got) {
		return errors.New("retired nonce identity collation changed")
	}
	if !IsSQLite(db) {
		want, e := nonceIdentityCollation(db, "traffic_records", "report_id")
		if e != nil {
			return e
		}
		got, e := nonceIdentityCollation(db, "metering_nonce_receipts", "report_id")
		if e != nil {
			return e
		}
		if want != got {
			return errors.New("retired report identity collation changed")
		}
	}
	var p model.MeteringLedgerRetentionProgress
	if e := db.First(&p, 1).Error; e != nil {
		return fmt.Errorf("ledger allocation/retention checkpoint missing: %w", e)
	}
	if p.AfterID > p.UpperID || p.HighWaterID < p.UpperID {
		return errors.New("ledger allocation high-water checkpoint is invalid")
	}
	return nil
}
func preflightLedgerRetentionSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version=?", ledgerRetentionVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateLedgerRetentionSchema(db)
}
func reconcileLedgerRetentionSchema(db *gorm.DB) error {
	if err := preflightLedgerRetentionSchema(db); err != nil {
		return err
	}
	asset := ledgerRetentionVersion
	if IsSQLite(db) {
		asset = "sqlite/0087_metering_ledger_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	for _, stmt := range statements {
		u := strings.ToUpper(strings.TrimSpace(stmt))
		switch {
		case strings.HasPrefix(u, "ALTER TABLE"):
			table, col := "traffic_records", "retention_level"

			if db.Migrator().HasColumn(table, col) {
				continue
			}
		case strings.HasPrefix(u, "CREATE TABLE"):
			table := "metering_nonce_receipts"
			if strings.Contains(stmt, "metering_ledger_targets") {
				table = "metering_ledger_targets"
			}
			if strings.Contains(stmt, "metering_ledger_retention_progress") {
				table = "metering_ledger_retention_progress"
			}
			if db.Migrator().HasTable(table) {
				continue
			}
			if table == "metering_nonce_receipts" {
				if !IsSQLite(db) {
					reportCollation, e := nonceIdentityCollation(db, "traffic_records", "report_id")
					if e != nil {
						return e
					}
					stmt = strings.Replace(stmt, "report_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci", "report_id VARCHAR(191) CHARACTER SET "+strings.Split(reportCollation, "_")[0]+" COLLATE "+reportCollation, 1)
				}
				c, err := nonceIdentityCollation(db, "traffic_records", "nonce")
				if err != nil {
					return err
				}
				if IsSQLite(db) {
					stmt = strings.Replace(stmt, "nonce TEXT NOT NULL", "nonce TEXT COLLATE "+c+" NOT NULL", 1)
				} else {
					stmt = strings.Replace(stmt, "nonce VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci", "nonce VARCHAR(64) CHARACTER SET "+strings.Split(c, "_")[0]+" COLLATE "+c, 1)
				}
			}
		default:
			return errors.New("unexpected ledger retention DDL")
		}
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if err := initializeLedgerRetentionProgress(db); err != nil {
		return err
	}
	if err := validateLedgerRetentionSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: ledgerRetentionVersion, AppliedAt: time.Now().UTC()}).Error
}

func initializeLedgerRetentionProgress(db *gorm.DB) error {
	var p model.MeteringLedgerRetentionProgress
	if e := db.First(&p, 1).Error; e == nil {
		return nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}
	var head struct{ ID uint }
	q := db.Table("metering_ledger_targets").Select("source_id AS id").Limit(1).Find(&head)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected > 0 {
		return errors.New("ledger allocation checkpoint missing with retired identities")
	}
	if db.Migrator().HasTable("metering_cold_roots") {
		var n int64
		if e := db.Model(&model.MeteringColdRoot{}).Where("kind=4").Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("ledger allocation checkpoint missing with sealed retired identities")
		}
	}
	if e := db.Table("traffic_records").Select("id").Order("id DESC").Limit(1).Find(&head).Error; e != nil {
		return e
	}
	return db.Create(&model.MeteringLedgerRetentionProgress{ID: 1, HighWaterID: head.ID, NextPassAt: time.Unix(0, 0).UTC()}).Error
}

// Reserve previously used source IDs without implicit-commit AUTO_INCREMENT
// DDL. The caller owns this empty destination transaction; the temporary zero
// row is never a business fact and is removed before any reader can observe it.
func preserveCopiedLedgerAllocator(tx *gorm.DB) error {
	var p model.MeteringLedgerRetentionProgress
	if e := tx.First(&p, 1).Error; e != nil {
		return e
	}
	var head struct{ ID uint }
	if e := tx.Table("traffic_records").Select("id").Order("id DESC").Limit(1).Find(&head).Error; e != nil {
		return e
	}
	if head.ID >= p.HighWaterID {
		return nil
	}
	now := time.Now().UTC()
	seed := map[string]any{"id": p.HighWaterID, "node_id": nil, "user_id": nil, "protocol_endpoint_id": nil, "subscription_id": nil, "report_id": nil, "nonce": nil, "record_at": time.Unix(0, 0).UTC(), "created_at": now, "updated_at": now, "raw_bytes": int64(0), "used_bytes": int64(0), "record_count": int64(0), "result_recorded": true, "retention_level": 0}
	if e := tx.Table("traffic_records").Create(seed).Error; e != nil {
		return e
	}
	return tx.Table("traffic_records").Where("id=?", p.HighWaterID).Delete(nil).Error
}
