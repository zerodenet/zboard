package datastore

import (
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

// ReconcileNodePublishSchema reuses the baseline DDL for older development databases.
func ReconcileNodePublishSchema(db *gorm.DB) error {
	if IsSQLite(db) {
		return nil
	}
	payload, err := migrations.Files.ReadFile(preReleaseBaselineVersion)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if strings.HasPrefix(statement, "CREATE TABLE `node_config_publishes`") {
			return db.Exec(strings.Replace(statement, "CREATE TABLE", "CREATE TABLE IF NOT EXISTS", 1)).Error
		}
	}
	return fmt.Errorf("node publication schema missing from baseline")
}
