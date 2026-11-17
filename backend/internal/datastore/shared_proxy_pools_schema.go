package datastore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const sharedProxyPoolsVersion = "0094_shared_proxy_pools.up.sql"

func preflightSharedProxyPools(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if e := db.Table("schema_migrations").Where("version=?", sharedProxyPoolsVersion).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 && !db.Migrator().HasTable(&model.NodeProxyPoolBinding{}) {
		return fmt.Errorf("recorded shared proxy pool references are missing; restore before startup")
	}
	return nil
}

func reconcileSharedProxyPools(db *gorm.DB) error {
	if err := preflightSharedProxyPools(db); err != nil {
		return err
	}
	var n int64
	if e := db.Table("schema_migrations").Where("version=?", sharedProxyPoolsVersion).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	if !IsSQLite(db) {
		if err := nativeSettlementDDL(db, "ALTER TABLE node_proxy_pools MODIFY COLUMN node_id bigint unsigned NULL, ALGORITHM=INPLACE, LOCK=NONE"); err != nil {
			return err
		}
	}
	if err := db.AutoMigrate(&model.NodeProxyPoolBinding{}); err != nil {
		return err
	}
	// Copy old ownership and actual path references before clearing ownership.
	// IDs, encrypted sources/configs, revisions and sync schedules are untouched.
	return db.Transaction(func(tx *gorm.DB) error {
		insert := "INSERT INTO node_proxy_pool_bindings(node_id,proxy_pool_id,created_at) SELECT node_id,id,? FROM node_proxy_pools WHERE node_id IS NOT NULL AND node_id>0"
		if IsSQLite(tx) {
			insert = "INSERT OR IGNORE" + insert[len("INSERT"):]
		} else {
			insert = "INSERT IGNORE" + insert[len("INSERT"):]
		}
		if e := tx.Exec(insert, time.Now().UTC()).Error; e != nil {
			return e
		}
		insert = "INSERT INTO node_proxy_pool_bindings(node_id,proxy_pool_id,created_at) SELECT DISTINCT node_id,proxy_pool_id,? FROM network_entries WHERE proxy_pool_id IS NOT NULL AND node_id IS NOT NULL AND node_id>0"
		if IsSQLite(tx) {
			insert = "INSERT OR IGNORE" + insert[len("INSERT"):]
		} else {
			insert = "INSERT IGNORE" + insert[len("INSERT"):]
		}
		if e := tx.Exec(insert, time.Now().UTC()).Error; e != nil {
			return e
		}
		if e := tx.Model(&model.NodeProxyPool{}).Where("node_id IS NOT NULL").UpdateColumn("node_id", nil).Error; e != nil {
			return e
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: sharedProxyPoolsVersion, AppliedAt: time.Now().UTC()}).Error
	})
}
