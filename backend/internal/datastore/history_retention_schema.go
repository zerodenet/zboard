package datastore

import (
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const historyRetentionIndexVersion = "0074_history_retention_indexes.up.sql"

func reconcileHistoryRetentionIndexes(db *gorm.DB) error {
	asset := historyRetentionIndexVersion
	if IsSQLite(db) {
		asset = "sqlite/0074_history_retention_indexes.sql"
	}
	data, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(data))
	if err != nil {
		return err
	}
	specs := []struct{ table, name, columns string }{
		{"job_runs", "idx_job_retention", "state,finished_at,id"},
		{"tasks", "idx_task_retention", "finished_at,id"},
		{"node_operations", "idx_node_operation_retention", "finished_at,id"},
		{"protocol_deployments", "idx_deployment_retention", "finished_at,id"},
		{"certificate_operations", "idx_certificate_operation_retention", "finished_at,id"},
		{"provider_operations", "idx_provider_operation_retention", "finished_at,id"},
	}
	if len(statements) != len(specs) {
		return fmt.Errorf("retention index migration shape differs")
	}
	for i, spec := range specs {
		if !db.Migrator().HasIndex(spec.table, spec.name) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
		indexes, err := projectionTableIndexes(db, spec.table)
		if err != nil {
			return err
		}
		found := false
		for _, idx := range indexes {
			found = found || !idx.unique && strings.EqualFold(idx.columns, spec.columns)
		}
		if !found {
			return fmt.Errorf("retention index shape differs: %s", spec.name)
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: historyRetentionIndexVersion, AppliedAt: time.Now().UTC()}).Error
}
