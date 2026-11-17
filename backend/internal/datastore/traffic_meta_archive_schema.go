package datastore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const trafficMetaArchiveVersion = "0064_traffic_record_meta_archives.up.sql"
const trafficMetaArchiveTable = "traffic_record_meta_archives"

func inspectTrafficMetaArchiveSchema(db *gorm.DB, prepared *bool) error {
	if db == nil {
		return errors.New("database is required")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", trafficMetaArchiveVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if !tables[trafficMetaArchiveTable] {
		if applied > 0 {
			return errors.New("recorded native proof archive missing; restore before startup/copy")
		}
		return nil
	}
	columns, primary, err := projectionColumns(db, trafficMetaArchiveTable)
	if err != nil {
		return err
	}
	if len(columns) != 8 || strings.Join(primary, ",") != "traffic_record_id" {
		return errors.New("native proof archive columns or primary key differ")
	}
	for _, name := range []string{"traffic_record_id", "version", "source_bytes", "source_sha256", "fact_sha256", "compressed_bytes", "payload", "created_at"} {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		switch name {
		case "source_sha256", "fact_sha256":
			kind = "varchar(64)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "payload":
			kind = "longblob"
			if IsSQLite(db) {
				kind = "blob"
			}
		case "created_at":
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		c, ok := columns[name]
		if !ok || c.kind != kind || c.nullable {
			return fmt.Errorf("native proof archive column %s differs", name)
		}
		if !IsSQLite(db) && (name == "source_sha256" || name == "fact_sha256") && c.collation != "ascii_bin" {
			return errors.New("native proof archive digest collation differs")
		}
	}
	// The archive is independent authority. It must not disappear through a
	// cascade if a future ledger maintenance path is introduced.
	var foreign int64
	if IsSQLite(db) {
		var refs []struct {
			Table string `gorm:"column:table"`
		}
		if err := db.Raw("PRAGMA foreign_key_list(traffic_record_meta_archives)").Scan(&refs).Error; err != nil {
			return err
		}
		foreign = int64(len(refs))
	} else if err := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL", trafficMetaArchiveTable).Scan(&foreign).Error; err != nil {
		return err
	}
	if foreign != 0 {
		return errors.New("native proof archive has unexpected foreign-key ownership")
	}
	if !tables["traffic_records"] {
		var head struct{ TrafficRecordID uint }
		r := db.Table(trafficMetaArchiveTable).Select("traffic_record_id").Limit(1).Scan(&head)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected > 0 {
			return errors.New("native proof archive ledger missing; restore source before startup/copy")
		}
	}
	if prepared != nil {
		*prepared = applied > 0
	}
	return nil
}

func reconcileTrafficMetaArchiveSchema(db *gorm.DB) error {
	if err := inspectTrafficMetaArchiveSchema(db, nil); err != nil {
		return err
	}
	if !db.Migrator().HasTable(trafficMetaArchiveTable) {
		asset := trafficMetaArchiveVersion
		if IsSQLite(db) {
			asset = "sqlite/0064_traffic_record_meta_archives.sql"
		}
		raw, err := migrations.Files.ReadFile(asset)
		if err != nil {
			return err
		}
		statements, err := splitMigrationStatements(string(raw))
		if err != nil || len(statements) != 1 {
			return errors.New("unexpected native proof archive migration inventory")
		}
		if err := db.Exec(statements[0]).Error; err != nil {
			return err
		}
	}
	if err := inspectTrafficMetaArchiveSchema(db, nil); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: trafficMetaArchiveVersion, AppliedAt: time.Now().UTC()}).Error
}

// Used before opening maintenance writers. No missing table, migration receipt
// or physical authority is repaired by an archive/read/restore operation.
func ValidateTrafficMetaArchiveDatabase(db *gorm.DB) error {
	if err := inspectTrafficMetaArchiveSchema(db, nil); err != nil {
		return err
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", trafficMetaArchiveVersion).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 || !db.Migrator().HasTable(trafficMetaArchiveTable) || !db.Migrator().HasTable("traffic_records") {
		return errors.New("native proof archive is not prepared")
	}
	return nil
}

// This explicit rollback maintenance already requires stopped writers and may
// inspect the historical ledger. Older binaries cannot hydrate the new format;
// retain the archive table and restore every live reference before downgrade.
// Only a bounded prefix and one matching ID are returned, never proof bodies.
func requireNoLiveTrafficMetaArchiveReferences(db *gorm.DB) error {
	if err := inspectTrafficMetaArchiveSchema(db, nil); err != nil {
		return err
	}
	if !db.Migrator().HasTable(trafficMetaArchiveTable) {
		return nil
	}
	// The immutable authority, not an editable Meta.type prefix, decides which
	// sources must have been restored. Inspect small headers in bounded pages;
	// never load compressed payloads or trust a renamed/malformed marker.
	var after uint
	for {
		var headers []model.TrafficMetaArchive
		if err := db.Model(&model.TrafficMetaArchive{}).Select("traffic_record_id,version,source_bytes,substr(source_sha256,1,65) AS source_sha256,substr(fact_sha256,1,65) AS fact_sha256,created_at").Where("traffic_record_id > ?", after).Order("traffic_record_id").Limit(32).Find(&headers).Error; err != nil {
			return err
		}
		for _, header := range headers {
			if header.TrafficRecordID == 0 || header.Version != 1 || header.SourceBytes == 0 || header.SourceBytes > 65535 || len(header.SourceSHA256) != 64 || len(header.FactSHA256) != 64 || header.CreatedAt.IsZero() {
				return errors.New("native proof authority invalid; restore verified evidence before hourly rollback")
			}
			var source model.TrafficRecord
			if err := db.Omit("meta").First(&source, header.TrafficRecordID).Error; err != nil {
				return errors.Join(errors.New("native proof source unavailable before hourly rollback"), err)
			}
			if metering.ProofFactDigest(metering.TrafficRecord(source)) != header.FactSHA256 {
				return errors.New("native proof fact binding differs before hourly rollback")
			}
			var size uint64
			if err := db.Model(&model.TrafficRecord{}).Select("octet_length(meta)").Where("id = ?", header.TrafficRecordID).Scan(&size).Error; err != nil {
				return err
			}
			if size != header.SourceBytes {
				return errors.New("restore all native proof material with the current binary before hourly rollback")
			}
			var live struct{ Meta string }
			expression := "substr(CAST(meta AS BLOB),1,?) AS meta"
			if !IsSQLite(db) {
				expression = "substr(CAST(meta AS BINARY),1,?) AS meta"
			}
			if err := db.Model(&model.TrafficRecord{}).Select(expression, int(header.SourceBytes)+1).Where("id = ?", header.TrafficRecordID).Take(&live).Error; err != nil {
				return err
			}
			digest := sha256.Sum256([]byte(live.Meta))
			if uint64(len(live.Meta)) != header.SourceBytes || hex.EncodeToString(digest[:]) != header.SourceSHA256 {
				return errors.New("restore all native proof material with the current binary before hourly rollback")
			}
			after = header.TrafficRecordID
		}
		if len(headers) < 32 {
			break
		}
	}
	// A surviving canonical reference without its header is also authority loss.
	const prefix = `{"type":"metering_native_proof_archive"`
	var row struct{ ID uint }
	result := db.Table("traffic_records").Select("id").Where("event_type <> '' AND substr(meta,1,?) = ?", len(prefix), prefix).Limit(1).Scan(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 0 {
		return errors.New("restore all native proof material with the current binary before hourly rollback")
	}
	return nil
}
