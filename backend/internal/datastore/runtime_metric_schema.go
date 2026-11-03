package datastore

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runtimeMetricHistoryVersion = "0082_runtime_metric_history.up.sql"

func validateRuntimeMetricHistory(db *gorm.DB) error {
	columns, primary, err := projectionColumns(db, "runtime_metric_minutes")
	if err != nil {
		return err
	}
	if len(columns) != 13 || !slices.Equal(primary, []string{"slot"}) {
		return fmt.Errorf("runtime metric history shape differs")
	}
	for _, name := range []string{"slot", "minute_at", "sample_count", "cpu_sum", "memory_sum", "disk_sum", "rx_sum", "tx_sum", "cpu_count", "memory_count", "disk_count", "rx_count", "tx_count"} {
		column, ok := columns[name]
		expected := "int unsigned"
		if strings.HasSuffix(name, "_sum") {
			expected = "double"
		}
		if name == "slot" {
			expected = "smallint unsigned"
		}
		if name == "sample_count" {
			expected = "bigint unsigned"
		}
		if name == "minute_at" {
			expected = "datetime(3)"
		}
		if IsSQLite(db) {
			expected = "integer"
			if strings.HasSuffix(name, "_sum") {
				expected = "real"
			}
			if name == "minute_at" {
				expected = "datetime"
			}
		}
		if !ok || column.nullable || column.kind != expected {
			return fmt.Errorf("runtime metric column missing/nullable: %s", name)
		}
	}
	if !db.Migrator().HasIndex("runtime_metric_minutes", "idx_runtime_metric_expiry") {
		return fmt.Errorf("runtime metric expiry index missing")
	}
	var ddl string
	if IsSQLite(db) {
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='runtime_metric_minutes'").Row().Scan(&ddl); err != nil {
			return err
		}
	} else {
		var table string
		if err := db.Raw("SHOW CREATE TABLE runtime_metric_minutes").Row().Scan(&table, &ddl); err != nil {
			return err
		}
	}
	compact := strings.ToLower(strings.NewReplacer("`", "", "\"", "", " ", "", "\n", "", "\t", "", "(", "", ")", "").Replace(ddl))
	if !strings.Contains(compact, "chk_runtime_metric_slot") || !strings.Contains(compact, "slot<1440") {
		return fmt.Errorf("runtime metric slot capacity constraint missing")
	}
	return nil
}

// Resume an interrupted additive DDL without replacing retained monitoring data.
// SQLite uses its native SQL asset, rather than silently relying on AutoMigrate.
func reconcileRuntimeMetricHistory(db *gorm.DB) error {
	if !db.Migrator().HasTable("runtime_metric_minutes") {
		asset := runtimeMetricHistoryVersion
		if IsSQLite(db) {
			asset = "sqlite/0082_runtime_metric_history.sql"
		}
		raw, err := migrations.Files.ReadFile(asset)
		if err != nil {
			return err
		}
		statements, err := splitMigrationStatements(string(raw))
		if err != nil {
			return err
		}
		for _, statement := range statements {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
	} else if !db.Migrator().HasIndex("runtime_metric_minutes", "idx_runtime_metric_expiry") {
		if err := db.Exec("CREATE INDEX idx_runtime_metric_expiry ON runtime_metric_minutes(minute_at)").Error; err != nil {
			return err
		}
	}
	if err := validateRuntimeMetricHistory(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: runtimeMetricHistoryVersion, AppliedAt: time.Now().UTC()}).Error
}

func preflightRuntimeMetricHistory(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version=?", runtimeMetricHistoryVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied > 0 {
		return validateRuntimeMetricHistory(db)
	}
	return nil
}
