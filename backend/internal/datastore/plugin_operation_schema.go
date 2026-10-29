package datastore

import (
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Plugin tables predate the logical model inventory. Reconcile only additive
// audit columns after the baseline SQL on both SQLite and MySQL. Missing
// columns after a partially applied MySQL DDL are safe to retry on startup.
func reconcilePluginOperationSchema(db *gorm.DB) error {
	if db.Dialector.Name() == DriverMySQL {
		if err := addMissingMySQLModelColumns(db, &model.PluginOperation{}, []string{"PreviousPublisher", "Publisher", "SigningKeyFingerprint", "PackageDigest"}); err != nil {
			return fmt.Errorf("reconcile plugin operation audit columns: %w", err)
		}
		return nil
	}
	for _, column := range []string{"PreviousPublisher", "Publisher", "SigningKeyFingerprint", "PackageDigest"} {
		if !db.Migrator().HasColumn(&model.PluginOperation{}, column) {
			if err := db.Migrator().AddColumn(&model.PluginOperation{}, column); err != nil {
				return fmt.Errorf("reconcile plugin operation %s: %w", column, err)
			}
		}
	}
	return nil
}
