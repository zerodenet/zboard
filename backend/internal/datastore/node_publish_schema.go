package datastore

import (
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

// ReconcileNodePublishSchema reuses the baseline DDL for older development databases.
func ReconcileNodePublishSchema(db *gorm.DB) error {
	if IsSQLite(db) {
		return nil
	}
	statements, err := nodePublicationBaselineStatements()
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if err := db.Exec(strings.Replace(statement, "CREATE TABLE", "CREATE TABLE IF NOT EXISTS", 1)).Error; err != nil {
			return err
		}
	}
	for _, column := range []struct {
		model any
		field string
	}{
		{&model.NodeConfigPublish{}, "Deferred"},
		{&model.NodeConfigPublish{}, "Reconciling"},
		{&model.NodeConfigPublish{}, "Suspended"},
		{&model.NetworkEntry{}, "ListenAddress"},
	} {
		if !db.Migrator().HasColumn(column.model, column.field) {
			if err := db.Migrator().AddColumn(column.model, column.field); err != nil {
				return fmt.Errorf("reconcile publication column %s: %w", column.field, err)
			}
		}
	}
	if !db.Migrator().HasColumn(&model.NetworkEntry{}, "DeliverySortOrder") {
		if err := db.Migrator().AddColumn(&model.NetworkEntry{}, "DeliverySortOrder"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&model.NetworkEntry{}, "ProxyPoolID") {
		if err := db.Migrator().AddColumn(&model.NetworkEntry{}, "ProxyPoolID"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(&model.NetworkEntry{}, "idx_network_entries_proxy_pool_id") {
		if err := db.Migrator().CreateIndex(&model.NetworkEntry{}, "ProxyPoolID"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasConstraint(&model.NetworkEntry{}, "fk_network_entries_proxy_pool") {
		if err := db.Migrator().CreateConstraint(&model.NetworkEntry{}, "ProxyPool"); err != nil {
			return err
		}
	}
	var nullable string
	if err := db.Raw("SELECT IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'protocol_deployments' AND COLUMN_NAME = 'protocol_endpoint_id'").Scan(&nullable).Error; err != nil {
		return err
	}
	if nullable == "YES" {
		return nil
	}
	return db.Exec("ALTER TABLE protocol_deployments MODIFY COLUMN protocol_endpoint_id bigint unsigned NULL DEFAULT NULL").Error
}

// The original table shape must exist before append-only column migrations.
// Current model columns are reconciled only after those migrations complete.
func nodePublicationBaselineStatements() ([]string, error) {
	payload, err := migrations.Files.ReadFile(preReleaseBaselineVersion)
	if err != nil {
		return nil, err
	}
	all, err := splitMigrationStatements(string(payload))
	if err != nil {
		return nil, err
	}
	var statements []string
	for _, statement := range all {
		if strings.HasPrefix(statement, "CREATE TABLE `node_proxy_pools`") || strings.HasPrefix(statement, "CREATE TABLE `node_config_publishes`") || strings.HasPrefix(statement, "CREATE TABLE `network_entries`") || strings.HasPrefix(statement, "CREATE TABLE `node_group_network_entries`") {
			statements = append(statements, statement)
		}
	}
	if len(statements) != 4 {
		return nil, fmt.Errorf("node publication or network entry schema missing from baseline")
	}
	return statements, nil
}
