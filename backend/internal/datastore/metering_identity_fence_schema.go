package datastore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const meteringIdentityFenceVersion = "0090_metering_identity_fences.up.sql"

func reconcileMeteringIdentityFenceSchema(db *gorm.DB) error {
	var applied int64
	if e := db.Table("schema_migrations").Where("version=?", meteringIdentityFenceVersion).Count(&applied).Error; e != nil {
		return e
	}
	if applied == 0 {
		if !db.Migrator().HasTable("metering_identity_fences") {
			asset := meteringIdentityFenceVersion
			if IsSQLite(db) {
				asset = "sqlite/0090_metering_identity_fences.sql"
			}
			raw, e := migrations.Files.ReadFile(asset)
			if e != nil {
				return e
			}
			statements, e := splitMigrationStatements(string(raw))
			if e != nil {
				return e
			}
			for _, stmt := range statements {
				if e := db.Exec(stmt).Error; e != nil {
					return e
				}
			}
		}
	}
	columns, pk, e := projectionColumns(db, "metering_identity_fences")
	if e != nil {
		return e
	}
	kind := "bigint unsigned"
	if IsSQLite(db) {
		kind = "integer"
	}
	if len(columns) != 1 || len(pk) != 1 || pk[0] != "node_id" || columns["node_id"].kind != kind || columns["node_id"].nullable {
		return errors.New("metering identity fence schema missing or changed")
	}
	if applied > 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: meteringIdentityFenceVersion, AppliedAt: time.Now().UTC()}).Error
}
