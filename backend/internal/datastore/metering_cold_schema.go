package datastore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"strings"
	"time"
)

const meteringColdVersion = "0088_metering_cold_identities.up.sql"

func currentIdentityProfiles(db *gorm.DB) ([]model.MeteringIdentityProfile, error) {
	profiles := []model.MeteringIdentityProfile{}
	for _, s := range []struct {
		kind       uint8
		table, col string
	}{{1, "traffic_records", "report_id"}, {2, "traffic_records", "nonce"}, {3, "flow_cursor_checkpoints", "flow_id"}} {
		c, e := nonceIdentityCollation(db, s.table, s.col)
		if e != nil {
			return nil, e
		}
		p := model.MeteringIdentityProfile{Kind: s.kind, Collation: c}
		if IsSQLite(db) {
			if c != "BINARY" && c != "NOCASE" && c != "RTRIM" {
				return nil, errors.New("unsupported SQLite replay identity collation")
			}
		} else {
			var attr string
			if e = db.Raw("SELECT PAD_ATTRIBUTE FROM information_schema.COLLATIONS WHERE COLLATION_NAME=?", c).Scan(&attr).Error; e != nil {
				return nil, e
			}
			if attr != "PAD SPACE" && attr != "NO PAD" {
				return nil, errors.New("replay identity padding unavailable")
			}
			p.PadSpace = attr == "PAD SPACE"
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}
func initializeColdProfiles(db *gorm.DB) error {
	expected, e := currentIdentityProfiles(db)
	if e != nil {
		return e
	}
	var n int64
	if e = db.Model(&model.MeteringIdentityProfile{}).Count(&n).Error; e != nil {
		return e
	}
	if n == 0 {
		var count int64
		if e = db.Model(&model.MeteringColdBlock{}).Count(&count).Error; e != nil {
			return e
		}
		if count != 0 {
			return errors.New("cold identity profiles missing with existing replay authority")
		}
		return db.Create(&expected).Error
	}
	var actual []model.MeteringIdentityProfile
	if e = db.Order("kind").Find(&actual).Error; e != nil {
		return e
	}
	if !slices.Equal(actual, expected) {
		return errors.New("cold identity collation differs; cross-collation copy requires explicit verified re-keying")
	}
	return nil
}
func validateMeteringColdSchema(db *gorm.DB) error {
	for _, s := range []struct {
		table string
		pk    []string
		cols  map[string]string
	}{
		{"metering_cold_roots", []string{"node_id", "kind"}, map[string]string{"node_id": "bigint unsigned", "kind": "tinyint unsigned", "page_count": "bigint", "entry_count": "bigint"}},
		{"metering_cold_blocks", []string{"id"}, map[string]string{"bloom": "varbinary(1024)", "header_sha256": "varchar(64)", "id": "bigint unsigned", "node_id": "bigint unsigned", "kind": "tinyint unsigned", "depth": "tinyint unsigned", "prefix": "int unsigned", "version": "tinyint unsigned", "entry_count": "int unsigned", "raw_bytes": "int unsigned", "sha256": "varchar(64)", "payload": "mediumblob"}},
		{"metering_identity_profiles", []string{"kind"}, map[string]string{"kind": "tinyint unsigned", "collation": "varchar(64)", "pad_space": "tinyint(1)"}},
		{"metering_cold_progress", []string{"kind"}, map[string]string{"held_count": "bigint unsigned", "last_held_node_id": "bigint unsigned", "last_hold_reason": "varchar(255)", "kind": "tinyint unsigned", "node_id": "bigint unsigned", "after_key": "varchar(191)", "after_id": "bigint unsigned", "next_pass_at": "datetime(3)"}},
	} {
		cols, pk, e := projectionColumns(db, s.table)
		if e != nil {
			return e
		}
		if len(cols) != len(s.cols) || !slices.Equal(pk, s.pk) {
			return errors.New("cold identity table shape differs: " + s.table)
		}
		for name, kind := range s.cols {
			if IsSQLite(db) {
				kind = "integer"
				if name == "last_hold_reason" || name == "header_sha256" || name == "sha256" || name == "collation" || name == "after_key" {
					kind = "text"
				}
				if name == "bloom" || name == "payload" {
					kind = "blob"
				}
				if name == "next_pass_at" {
					kind = "datetime"
				}
			}
			c, ok := cols[name]
			if !ok || c.nullable || c.kind != kind {
				return fmt.Errorf("cold identity column differs: %s.%s", s.table, name)
			}
		}
	}
	ok, e := principalStatisticsIndex(db, principalStatisticsIndexSpec{"metering_cold_blocks", "uk_metering_cold_page", []string{"node_id", "kind", "depth", "prefix"}, true})
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("cold identity directory unique index missing")
	}
	var actual []model.MeteringIdentityProfile
	if e = db.Order("kind").Find(&actual).Error; e != nil {
		return e
	}
	expected, e := currentIdentityProfiles(db)
	if e != nil {
		return e
	}
	if !slices.Equal(actual, expected) {
		return errors.New("recorded replay collation profile changed or missing")
	}
	return nil
}
func preflightMeteringColdSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if e := db.Table("schema_migrations").Where("version=?", meteringColdVersion).Count(&n).Error; e != nil {
		return e
	}
	if n == 0 {
		return nil
	}
	return validateMeteringColdSchema(db)
}
func reconcileMeteringColdSchema(db *gorm.DB) error {
	if e := preflightMeteringColdSchema(db); e != nil {
		return e
	}
	asset := meteringColdVersion
	if IsSQLite(db) {
		asset = "sqlite/0088_metering_cold_identities.sql"
	}
	raw, e := migrations.Files.ReadFile(asset)
	if e != nil {
		return e
	}
	stmts, e := splitMigrationStatements(string(raw))
	if e != nil {
		return e
	}
	for _, stmt := range stmts {
		u := strings.ToUpper(strings.TrimSpace(stmt))
		if strings.HasPrefix(u, "CREATE TABLE") {
			table := "metering_cold_blocks"
			if strings.Contains(stmt, "metering_cold_roots") {
				table = "metering_cold_roots"
			}
			if strings.Contains(stmt, "metering_identity_profiles") {
				table = "metering_identity_profiles"
			}
			if strings.Contains(stmt, "metering_cold_progress") {
				table = "metering_cold_progress"
			}
			if db.Migrator().HasTable(table) {
				continue
			}
		} else if strings.HasPrefix(u, "CREATE UNIQUE INDEX") {
			if db.Migrator().HasIndex("metering_cold_blocks", "uk_metering_cold_page") {
				continue
			}
		} else {
			return errors.New("unexpected cold identity DDL")
		}
		if e = db.Exec(stmt).Error; e != nil {
			return e
		}
	}
	if e = initializeColdProfiles(db); e != nil {
		return e
	}
	if e = validateMeteringColdSchema(db); e != nil {
		return e
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: meteringColdVersion, AppliedAt: time.Now().UTC()}).Error
}
