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
	payload, err := migrations.Files.ReadFile(preReleaseBaselineVersion)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	found := 0
	for _, statement := range statements {
		if strings.HasPrefix(statement, "CREATE TABLE `node_proxy_pools`") || strings.HasPrefix(statement, "CREATE TABLE `node_config_publishes`") || strings.HasPrefix(statement, "CREATE TABLE `network_entries`") || strings.HasPrefix(statement, "CREATE TABLE `node_group_network_entries`") {
			found++
			if err := db.Exec(strings.Replace(statement, "CREATE TABLE", "CREATE TABLE IF NOT EXISTS", 1)).Error; err != nil {
				return err
			}
		}
	}
	if found != 4 {
		return fmt.Errorf("node publication or network entry schema missing from baseline")
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
