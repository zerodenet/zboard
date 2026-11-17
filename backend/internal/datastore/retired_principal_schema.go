package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const retiredPrincipalSchemaVersion = "0044_retired_principal_hash.up.sql"

func preflightRetiredPrincipalSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", retiredPrincipalSchemaVersion).Count(&applied).Error; err != nil {
		return err
	}
	if applied > 0 && (!db.Migrator().HasTable("protocol_credentials") || !db.Migrator().HasColumn("protocol_credentials", "retired_principal_hash")) {
		return fmt.Errorf("retired principal recovery evidence missing after migration; restore before startup")
	}
	return nil
}
func reconcileRetiredPrincipalSchema(db *gorm.DB) error {
	if err := preflightRetiredPrincipalSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable(&model.ProtocolCredential{}) {
		return fmt.Errorf("retired principal recovery evidence requires credentials")
	}
	if !db.Migrator().HasColumn(&model.ProtocolCredential{}, "RetiredPrincipalHash") {
		path := retiredPrincipalSchemaVersion
		if IsSQLite(db) {
			path = "sqlite/0044_retired_principal_hash.sql"
		}
		payload, err := migrations.Files.ReadFile(path)
		if err != nil {
			return err
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			return err
		}
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(&model.ProtocolCredential{}, "idx_credential_retired_principal") {
		if err := db.Exec("CREATE INDEX idx_credential_retired_principal ON protocol_credentials(node_id,retired_principal_hash)").Error; err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: retiredPrincipalSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
