package datastore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const publicationConnectionSchemaVersion = "0045_publication_connection_phase.up.sql"

func reconcilePublicationConnectionSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.NodeConfigPublish{}) {
		if err := ReconcileNodePublishSchema(db); err != nil {
			return err
		}
	}
	if db.Dialector.Name() == DriverMySQL {
		if err := addMissingMySQLModelColumns(db, &model.NodeConfigPublish{}, []string{"Phase", "AppliedConfigSHA256"}); err != nil {
			return err
		}
	} else {
		for _, field := range []string{"Phase", "AppliedConfigSHA256"} {
			if !db.Migrator().HasColumn(&model.NodeConfigPublish{}, field) {
				if err := db.Migrator().AddColumn(&model.NodeConfigPublish{}, field); err != nil {
					return err
				}
			}
		}
	}
	// Loss of auxiliary phase evidence never proves a configuration applied.
	// Re-enter the existing exact-SHA probe/reconciliation path before closing.
	if err := db.Model(&model.NodeConfigPublish{}).Where("phase = ? AND applied_config_sha256 = ?", "connections", "").Update("phase", "").Error; err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: publicationConnectionSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
