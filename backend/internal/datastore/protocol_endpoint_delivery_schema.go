package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const protocolEndpointDeliveryVersion = "0095_protocol_endpoint_delivery.up.sql"

func reconcileProtocolEndpointDeliverySchema(db *gorm.DB) error {
	var recorded int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", protocolEndpointDeliveryVersion).Count(&recorded).Error; err != nil {
		return err
	}
	path := protocolEndpointDeliveryVersion
	if IsSQLite(db) {
		path = "sqlite/0095_protocol_endpoint_delivery.sql"
	}
	payload, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(payload))
	if err != nil {
		return err
	}
	columns := []struct{ table, name string }{
		{"protocol_endpoints", "delivery_revision"},
		{"protocol_deployments", "protocol_endpoint_targets"},
	}
	if len(statements) != len(columns) {
		return fmt.Errorf("invalid protocol endpoint delivery migration")
	}
	for i, column := range columns {
		if db.Migrator().HasColumn(column.table, column.name) {
			continue
		}
		if recorded != 0 {
			return fmt.Errorf("applied protocol endpoint delivery migration is missing %s.%s", column.table, column.name)
		}
		if err := db.Exec(statements[i]).Error; err != nil {
			return fmt.Errorf("install %s.%s: %w", column.table, column.name, err)
		}
	}
	if recorded != 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: protocolEndpointDeliveryVersion, AppliedAt: time.Now().UTC()}).Error
}
