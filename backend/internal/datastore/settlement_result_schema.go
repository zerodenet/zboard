package datastore

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const settlementResultVersion = "0086_metering_settlement_results.up.sql"

func validateSettlementResultSchema(db *gorm.DB) error {
	cols, _, err := projectionColumns(db, "traffic_records")
	if err != nil {
		return err
	}
	c, ok := cols["result_recorded"]
	kind := "tinyint(1)"
	if IsSQLite(db) {
		kind = "integer"
	}
	def := strings.Trim(strings.ToLower(c.defaultSQL), "()\"' ")
	if !ok || c.nullable || c.kind != kind || (def != "0" && def != "false") {
		return errors.New("settlement source coverage column missing")
	}
	for _, spec := range []struct {
		table       string
		columns, pk []string
	}{
		{"metering_settlement_results", []string{"id", "result_key", "user_id", "subscription_id", "entitlement_epoch", "node_id", "protocol_endpoint_id", "traffic_calc_mode", "protocol_multiplier_milli", "raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"}, []string{"id"}},
		{"metering_settlement_progress", []string{"id", "after_id", "upper_id"}, []string{"id"}},
	} {
		cols, pk, err := projectionColumns(db, spec.table)
		if err != nil {
			return err
		}
		if len(cols) != len(spec.columns) || !slices.Equal(pk, spec.pk) {
			return errors.New("settlement result table shape differs: " + spec.table)
		}
		for _, name := range spec.columns {
			c, ok := cols[name]
			kind := "bigint"
			if name == "id" || name == "user_id" || name == "subscription_id" || name == "entitlement_epoch" || name == "node_id" || name == "protocol_endpoint_id" || name == "after_id" || name == "upper_id" {
				kind = "bigint unsigned"
			}
			if name == "result_key" {
				kind = "char(64)"
			}
			if name == "traffic_calc_mode" {
				kind = "smallint"
			}
			if IsSQLite(db) {
				kind = "integer"
				if name == "result_key" {
					kind = "text"
				}
			}
			if !ok || c.nullable || c.kind != kind {
				return errors.New("settlement result column differs: " + name)
			}
		}
	}
	ok, err = principalStatisticsIndex(db, principalStatisticsIndexSpec{"metering_settlement_results", "uk_settlement_result_key", []string{"result_key"}, true})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("settlement result identity unique index missing")
	}
	ok, err = principalStatisticsIndex(db, principalStatisticsIndexSpec{"metering_settlement_results", "idx_settlement_result_subscription", []string{"subscription_id"}, false})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("settlement result subscription index missing")
	}
	var p model.MeteringSettlementProgress
	if err := db.First(&p, 1).Error; err != nil {
		return err
	}
	if p.AfterID > p.UpperID {
		return errors.New("settlement result prefix invalid")
	}
	return nil
}
func preflightSettlementResultSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", settlementResultVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateSettlementResultSchema(db)
}
func reconcileSettlementResultSchema(db *gorm.DB) error {
	if err := preflightSettlementResultSchema(db); err != nil {
		return err
	}
	asset := settlementResultVersion
	if IsSQLite(db) {
		asset = "sqlite/0086_metering_settlement_results.sql"
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
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		switch {
		case strings.HasPrefix(upper, "ALTER TABLE"):
			if db.Migrator().HasColumn("traffic_records", "result_recorded") {
				continue
			}
		case strings.HasPrefix(upper, "CREATE TABLE"):
			table := "metering_settlement_results"
			if strings.Contains(stmt, "metering_settlement_progress") {
				table = "metering_settlement_progress"
			}
			if db.Migrator().HasTable(table) {
				continue
			}
		case strings.HasPrefix(upper, "CREATE INDEX"), strings.HasPrefix(upper, "CREATE UNIQUE INDEX"):
			name := "idx_settlement_result_subscription"
			if strings.Contains(stmt, "uk_settlement_result_key") {
				name = "uk_settlement_result_key"
			}
			if db.Migrator().HasIndex("metering_settlement_results", name) {
				continue
			}
		default:
			return errors.New("unexpected settlement result DDL")
		}
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	var p model.MeteringSettlementProgress
	err = db.First(&p, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var result struct{ Key string }
		q := db.Table("metering_settlement_results").Select("result_key AS `key`").Limit(1).Scan(&result)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 0 {
			return errors.New("settlement progress missing with existing results")
		}
		var last struct{ ID uint }
		if err := db.Table("traffic_records").Select("id").Order("id DESC").Limit(1).Find(&last).Error; err != nil {
			return err
		}
		p = model.MeteringSettlementProgress{ID: 1, UpperID: last.ID}
		if err := db.Create(&p).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := validateSettlementResultSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: settlementResultVersion, AppliedAt: time.Now().UTC()}).Error
}

func initializeCopiedSettlementResults(target *gorm.DB, sourceHasResults, sourceHasProgress bool) error {
	if sourceHasResults != sourceHasProgress {
		return errors.New("incomplete source settlement result authority")
	}
	if sourceHasProgress {
		return validateSettlementResultSchema(target)
	}
	var last struct{ ID uint }
	if err := target.Table("traffic_records").Select("id").Order("id DESC").Limit(1).Find(&last).Error; err != nil {
		return err
	}
	if err := target.Create(&model.MeteringSettlementProgress{ID: 1, UpperID: last.ID}).Error; err != nil {
		return err
	}
	return validateSettlementResultSchema(target)
}
