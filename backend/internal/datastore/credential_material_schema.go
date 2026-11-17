package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const credentialMaterialVersion = "0093_subscription_credential_material.up.sql"

// Recorded public verifiers and checkpoint ingress are financial attribution
// authority. Never let AutoMigrate silently recreate a dropped source column.
func preflightCredentialMaterialSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if e := db.Table("schema_migrations").Where("version=?", credentialMaterialVersion).Count(&n).Error; e != nil {
		return e
	}
	if n == 0 {
		return nil
	}
	for table, columns := range map[string][]string{
		"protocol_credentials": {"material_group_id", "material_version", "material_scope", "material_verifier", "compatibility_started_at", "compatibility_until"},
		"subscriptions":        {"credential_material_mode"}, "flow_cursor_checkpoints": {"protocol_endpoint_id"},
	} {
		for _, column := range columns {
			if !db.Migrator().HasColumn(table, column) {
				return fmt.Errorf("recorded credential attribution missing: %s.%s; restore before startup", table, column)
			}
		}
	}
	for _, index := range []string{"ux_subscription_credential_material", "idx_credential_compatibility_due"} {
		if !db.Migrator().HasIndex("protocol_credentials", index) {
			return fmt.Errorf("recorded credential material index missing: %s", index)
		}
	}
	columns, err := db.Migrator().ColumnTypes("protocol_credentials")
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() == "node_id" || column.Name() == "protocol_endpoint_id" {
			nullable, known := column.Nullable()
			if !known || !nullable {
				return fmt.Errorf("credential root parent must remain nullable: %s", column.Name())
			}
		}
	}
	return nil
}

func reconcileCredentialMaterialSchema(db *gorm.DB) error {
	var applied int64
	if err := db.Table("schema_migrations").Where("version=?", credentialMaterialVersion).Count(&applied).Error; err != nil {
		return err
	}
	for _, c := range []struct{ name, mysql, sqlite string }{
		{"material_group_id", "bigint unsigned NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
		{"material_version", "smallint unsigned NOT NULL DEFAULT 0", "INTEGER NOT NULL DEFAULT 0"},
		{"material_scope", "tinyint unsigned NULL", "INTEGER NULL"},
		{"material_verifier", "varchar(64) NOT NULL DEFAULT ''", "TEXT NOT NULL DEFAULT ''"},
		{"compatibility_started_at", "datetime(3) NULL", "datetime NULL"},
		{"compatibility_until", "datetime(3) NULL", "datetime NULL"},
	} {
		if db.Migrator().HasColumn("protocol_credentials", c.name) {
			continue
		}
		if applied > 0 {
			return fmt.Errorf("recorded subscription credential column missing: %s", c.name)
		}
		kind := c.mysql
		if IsSQLite(db) {
			kind = c.sqlite
		}
		if err := db.Exec("ALTER TABLE protocol_credentials ADD COLUMN " + c.name + " " + kind).Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn("subscriptions", "credential_material_mode") {
		if applied > 0 {
			return fmt.Errorf("recorded subscription credential mode missing")
		}
		if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN credential_material_mode INTEGER NOT NULL DEFAULT 0").Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn("flow_cursor_checkpoints", "protocol_endpoint_id") {
		if applied > 0 {
			return fmt.Errorf("recorded flow checkpoint ingress missing")
		}
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "INTEGER"
		}
		if err := db.Exec("ALTER TABLE flow_cursor_checkpoints ADD COLUMN protocol_endpoint_id " + kind + " NOT NULL DEFAULT 0").Error; err != nil {
			return err
		}
	}
	// The credential remains the stable financial identity, but a subscription
	// root has no physical node/endpoint parent. Existing rows retain their IDs
	// and foreign keys; no flow cursor or ledger is rewritten or deleted.
	if !IsSQLite(db) {
		for _, col := range []string{"node_id", "protocol_endpoint_id"} {
			var nullable string
			if err := db.Raw("SELECT is_nullable FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='protocol_credentials' AND column_name=?", col).Scan(&nullable).Error; err != nil {
				return err
			}
			if nullable == "YES" {
				continue
			}
			if applied > 0 {
				return fmt.Errorf("subscription credential parent nullability regressed: %s", col)
			}
			if err := nativeSettlementDDL(db, "ALTER TABLE protocol_credentials MODIFY COLUMN "+col+" bigint unsigned NULL, ALGORITHM=INPLACE, LOCK=NONE"); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasIndex("protocol_credentials", "ux_subscription_credential_material") {
		if applied > 0 {
			return fmt.Errorf("subscription credential material index missing")
		}
		if err := db.Exec("CREATE UNIQUE INDEX ux_subscription_credential_material ON protocol_credentials(subscription_id,entitlement_epoch,material_scope)").Error; err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex("protocol_credentials", "idx_credential_compatibility_due") {
		if applied > 0 {
			return fmt.Errorf("credential compatibility due index missing")
		}
		if err := db.Exec("CREATE INDEX idx_credential_compatibility_due ON protocol_credentials(material_version,compatibility_until,status,id)").Error; err != nil {
			return err
		}
	}
	for _, value := range CredentialMaterialDefaults() {
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "config_key"}}, DoNothing: true}).Create(&value).Error; err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: credentialMaterialVersion, AppliedAt: time.Now().UTC()}).Error
}

// CredentialMaterialDefaults is the exact initializer seed, also used to verify
// that an owned migration destination contains no operator changes.
func CredentialMaterialDefaults() []model.SystemConfig {
	return []model.SystemConfig{
		{ConfigKey: "subscription_credential_material_enabled", Name: "订阅凭证合并", Value: "false", ValueType: "bool", Description: "启用后新订阅使用一份主凭证；存量订阅需单独开始兼容迁移。关闭不会自动回滚已迁移订阅。", Revision: 1},
		{ConfigKey: "subscription_credential_compatibility_days", Name: "旧凭证兼容窗口（天）", Value: "30", ValueType: "int", Description: "节点确认双凭证配置后开始计时；默认30天，允许1至365天。修改仅影响尚未开始的窗口。", Revision: 1},
	}
}
