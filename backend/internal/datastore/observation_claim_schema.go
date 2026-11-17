package datastore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const observationClaimVersion = "0089_observation_claim_lease.up.sql"

func reconcileObservationClaimSchema(db *gorm.DB) error {
	var applied int64
	if e := db.Table("schema_migrations").Where("version=?", observationClaimVersion).Count(&applied).Error; e != nil {
		return e
	}
	spec := principalStatisticsIndexSpec{"node_observation_intents", "idx_observation_claim_lease", []string{"status", "lease_until", "id"}, false}
	if !db.Migrator().HasIndex(spec.Table, spec.Name) {
		if applied > 0 {
			return errors.New("recorded observation lease index missing")
		}
		asset := observationClaimVersion
		if IsSQLite(db) {
			asset = "sqlite/0089_observation_claim_lease.sql"
		}
		raw, e := migrations.Files.ReadFile(asset)
		if e != nil {
			return e
		}
		if e := db.Exec(string(raw)).Error; e != nil {
			return e
		}
	}
	valid, e := principalStatisticsIndex(db, spec)
	if e != nil {
		return e
	}
	if !valid {
		return errors.New("observation lease index shape changed")
	}
	if applied > 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: observationClaimVersion, AppliedAt: time.Now().UTC()}).Error
}
