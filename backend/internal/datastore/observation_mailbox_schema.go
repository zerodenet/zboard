package datastore

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const observationMailboxVersion = "0085_observation_mailboxes.up.sql"

func validateObservationMailboxSchema(db *gorm.DB) error {
	gc, keys, err := projectionColumns(db, "node_observation_gc_states")
	if err != nil {
		return err
	}
	if len(gc) != 3 || !slices.Equal(keys, []string{"id"}) {
		return errors.New("observation GC cursor shape differs")
	}
	for _, name := range []string{"id", "after_id", "next_pass_at"} {
		if c, ok := gc[name]; !ok || c.nullable {
			return errors.New("observation GC cursor column differs: " + name)
		}
	}
	cols, pk, e := projectionColumns(db, "node_observation_mailboxes")
	if e != nil {
		return e
	}
	if len(cols) != 7 || !slices.Equal(pk, []string{"node_id"}) {
		return errors.New("observation mailbox shape differs")
	}
	for _, k := range []string{"node_id", "asset_tag", "observation", "pending", "pending_since", "reason", "updated_at"} {
		if c, ok := cols[k]; !ok || c.nullable {
			return errors.New("observation mailbox column missing or nullable: " + k)
		}
	}
	state, _, err := projectionColumns(db, "node_observation_states")
	if err != nil {
		return err
	}
	for _, k := range []string{"latest_inline", "stats_inline"} {
		if c, ok := state[k]; !ok || c.nullable || c.kind != "text" {
			return errors.New("inline observation state differs: " + k)
		}
	}
	if cols["observation"].kind != "text" {
		return errors.New("observation mailbox must use bounded text")
	}
	if IsSQLite(db) {
		if cols["node_id"].kind != "integer" || gc["id"].kind != "integer" || gc["after_id"].kind != "text" {
			return errors.New("observation mailbox key type differs")
		}
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='node_observation_mailboxes'").Scan(&ddl).Error; err != nil {
			return err
		}
		normal := strings.ReplaceAll(strings.ToLower(ddl), " ", "")
		if !strings.Contains(normal, "check(length(cast(observationasblob))<=4096)") {
			return errors.New("observation mailbox byte limit missing")
		}
		var fks []struct {
			Table    string
			From     string
			To       string
			OnDelete string
		}
		if err := db.Raw("PRAGMA foreign_key_list(node_observation_mailboxes)").Scan(&fks).Error; err != nil {
			return err
		}
		found := false
		for _, fk := range fks {
			found = found || (fk.Table == "nodes" && fk.From == "node_id" && fk.To == "id" && fk.OnDelete == "CASCADE")
		}
		if !found {
			return errors.New("observation mailbox node ownership missing")
		}
	} else {
		if cols["node_id"].kind != "bigint unsigned" || gc["id"].kind != "bigint unsigned" || gc["after_id"].kind != "varchar(64)" || cols["asset_tag"].collation != "ascii_bin" {
			return errors.New("observation mailbox key type or collation differs")
		}
		var checks int64
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME='node_observation_mailboxes' AND CONSTRAINT_NAME='ck_observation_mailbox_bytes' AND CONSTRAINT_TYPE='CHECK' AND ENFORCED='YES'").Scan(&checks).Error; err != nil {
			return err
		}
		if checks != 1 {
			return errors.New("observation mailbox byte limit missing")
		}
		var clause string
		if err := db.Raw("SELECT CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND CONSTRAINT_NAME='ck_observation_mailbox_bytes'").Scan(&clause).Error; err != nil {
			return err
		}
		normal := strings.NewReplacer(" ", "", "`", "", "(", "", ")", "").Replace(strings.ToLower(clause))
		if normal != "octet_lengthobservation<=4096" && normal != "lengthobservation<=4096" {
			return errors.New("observation mailbox byte limit differs")
		}
		var fks int64
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME='node_observation_mailboxes' AND k.COLUMN_NAME='node_id' AND k.REFERENCED_TABLE_NAME='nodes' AND k.REFERENCED_COLUMN_NAME='id' AND r.DELETE_RULE='CASCADE'").Scan(&fks).Error; err != nil {
			return err
		}
		if fks != 1 {
			return errors.New("observation mailbox node ownership missing")
		}
	}
	ok, e := principalStatisticsIndex(db, principalStatisticsIndexSpec{"node_observation_mailboxes", "idx_observation_mailbox_pending", []string{"pending", "pending_since", "node_id"}, false})
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("observation mailbox pending index differs")
	}
	return nil
}
func preflightObservationMailboxSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var n int64
	if e := db.Table("schema_migrations").Where("version = ?", observationMailboxVersion).Count(&n).Error; e != nil {
		return e
	}
	if n == 0 {
		return nil
	}
	return validateObservationMailboxSchema(db)
}
func reconcileObservationMailboxSchema(db *gorm.DB) error {
	if e := preflightObservationMailboxSchema(db); e != nil {
		return e
	}
	asset := observationMailboxVersion
	if IsSQLite(db) {
		asset = "sqlite/0085_observation_mailboxes.sql"
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
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		switch {
		case strings.HasPrefix(upper, "ALTER TABLE"):
			column := "latest_inline"
			if strings.Contains(stmt, "stats_inline") {
				column = "stats_inline"
			}
			if db.Migrator().HasColumn("node_observation_states", column) {
				continue
			}
		case strings.HasPrefix(upper, "CREATE TABLE"):
			table := "node_observation_mailboxes"
			if strings.Contains(stmt, "node_observation_gc_states") {
				table = "node_observation_gc_states"
			}
			if db.Migrator().HasTable(table) {
				continue
			}
		case strings.HasPrefix(upper, "CREATE INDEX"):
			if db.Migrator().HasIndex("node_observation_mailboxes", "idx_observation_mailbox_pending") {
				continue
			}
		default:
			return errors.New("unexpected mailbox migration statement")
		}
		if e := db.Exec(stmt).Error; e != nil {
			return e
		}
	}
	if e := validateObservationMailboxSchema(db); e != nil {
		return e
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: observationMailboxVersion, AppliedAt: time.Now().UTC()}).Error
}
