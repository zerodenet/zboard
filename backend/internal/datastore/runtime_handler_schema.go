package datastore

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runtimeHandlerIndexVersion = "0081_runtime_handler_index.up.sql"

// The index can be prepared online before cutover. Reconcile DDL that committed
// before its migration receipt, and never fall back to a blocking table copy.
func reconcileRuntimeHandlerIndex(db *gorm.DB) error {
	if !db.Migrator().HasIndex("job_runs", "job_runtime_handler") {
		ddl := "CREATE INDEX job_runtime_handler ON job_runs (owner, handler, finished_at)"
		if !IsSQLite(db) {
			ddl = "ALTER TABLE job_runs ADD INDEX job_runtime_handler (owner, handler, finished_at), ALGORITHM=INPLACE, LOCK=NONE"
		}
		if err := db.Exec(ddl).Error; err != nil {
			return fmt.Errorf("prepare online runtime handler index: %w", err)
		}
	}
	if err := validateRuntimeHandlerIndex(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: runtimeHandlerIndexVersion, AppliedAt: time.Now().UTC()}).Error
}

func validateRuntimeHandlerIndex(db *gorm.DB) error {
	indexes, err := db.Migrator().GetIndexes("job_runs")
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, known := index.Unique()
		columns := index.Columns()
		if index.Name() == "job_runtime_handler" && known && !unique && len(columns) == 3 && columns[0] == "owner" && columns[1] == "handler" && columns[2] == "finished_at" {
			// Reject prefix, expression, descending and partial variants too.
			shapes, err := projectionTableIndexes(db, "job_runs")
			if err != nil {
				return err
			}
			for _, shape := range shapes {
				if shape.columns == "owner,handler,finished_at" && !shape.unique {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("runtime handler index missing or incompatible")
}
