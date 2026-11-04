package datastore

import (
	"errors"
	"slices"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const flowDetailRetentionVersion = "0084_flow_detail_retention.up.sql"

func validateFlowDetailRetentionSchema(db *gorm.DB) error {
	cols, pk, err := projectionColumns(db, "flow_detail_retention_states")
	if err != nil {
		return err
	}
	if len(cols) != 3 || !slices.Equal(pk, []string{"id"}) {
		return errors.New("flow detail retention checkpoint differs")
	}
	kind := "bigint unsigned"
	if IsSQLite(db) {
		kind = "integer"
	}
	for _, name := range []string{"id", "upper_id", "after_id"} {
		if col, ok := cols[name]; !ok || col.nullable || col.kind != kind {
			return errors.New("flow detail retention checkpoint column differs")
		}
	}
	valid, err := principalStatisticsIndex(db, principalStatisticsIndexSpec{"flow_start_statistics_minutes", "idx_flow_stats_minute_retention", []string{"minute_at", "subscription_id", "node_id", "basis"}, false})
	if err != nil {
		return err
	}
	if !valid {
		return errors.New("flow minute expiry index missing or differs")
	}
	return nil
}

func preflightFlowDetailRetentionSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if err := db.Table("schema_migrations").Where("version = ?", flowDetailRetentionVersion).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return validateFlowDetailRetentionSchema(db)
}

func reconcileFlowDetailRetentionSchema(db *gorm.DB) error {
	if err := preflightFlowDetailRetentionSchema(db); err != nil {
		return err
	}
	asset := flowDetailRetentionVersion
	if IsSQLite(db) {
		asset = "sqlite/0084_flow_detail_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	stmts, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	if len(stmts) != 2 {
		return errors.New("unexpected flow detail retention migration inventory")
	}
	if !db.Migrator().HasTable("flow_detail_retention_states") {
		if err := db.Exec(stmts[0]).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex("flow_start_statistics_minutes", "idx_flow_stats_minute_retention") {
		statement := stmts[1]
		// Explicit online secondary-index preparation: fail rather than silently
		// copy/rebuild a large live table, and bound metadata-lock waiting.
		if !IsSQLite(db) {
			statement += " ALGORITHM=INPLACE LOCK=NONE"
		}
		if err := nativeSettlementDDL(db, statement); err != nil {
			return err
		}
	}
	if err := validateFlowDetailRetentionSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: flowDetailRetentionVersion, AppliedAt: time.Now().UTC()}).Error
}
