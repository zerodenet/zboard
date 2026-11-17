package datastore_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteNativeProofArchiveMigrationAuthorityAndCopy(t *testing.T) {
	assertProofArchiveMigration(t, false)
}
func TestMySQLNativeProofArchiveMigrationAuthorityAndCopy(t *testing.T) {
	assertProofArchiveMigration(t, true)
}

func assertProofArchiveMigration(t *testing.T, mysql bool) {
	source := diagnosticSchemaDB(t, mysql)
	fact := diagnosticSchemaSource(t, source, false)
	const table = "traffic_record_meta_archives"
	const version = "0064_traffic_record_meta_archives.up.sql"
	body := []byte(strings.Repeat("unchanged proof", 100))
	var packed bytes.Buffer
	w := gzip.NewWriter(&packed)
	_, _ = w.Write(body)
	_ = w.Close()
	sum := sha256.Sum256(body)
	header := model.TrafficMetaArchive{TrafficRecordID: fact.ID, Version: 1, SourceBytes: uint64(len(body)), SourceSHA256: hex.EncodeToString(sum[:]), FactSHA256: strings.Repeat("a", 64), CompressedBytes: uint64(packed.Len()), Payload: packed.Bytes(), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := source.Create(&header).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(source); err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.ValidateTrafficMetaArchiveDatabase(source); err != nil {
		t.Fatal(err)
	}
	target := diagnosticSchemaDB(t, mysql)
	// RunMigrations seeds system settings. Exercise the real copy contract with
	// this test's exclusively owned destination, including its clearing/locking
	// transaction; a migrated database is not an empty copy destination.
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal("actual archive copy", err)
	}
	var copied model.TrafficMetaArchive
	if err := target.First(&copied, fact.ID).Error; err != nil || !bytes.Equal(copied.Payload, header.Payload) || copied.SourceSHA256 != header.SourceSHA256 || copied.FactSHA256 != header.FactSHA256 {
		t.Fatal("archive proof not preserved by copy", err)
	}
	if err := source.Exec("DROP TABLE " + table).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil || !strings.Contains(err.Error(), "proof archive missing") {
		t.Fatal("lost recorded authority recreated", err)
	}
	if source.Migrator().HasTable(table) {
		t.Fatal("lost payloads reconstructed as empty")
	}
	if err := datastore.CopyApplicationData(source, target); err == nil {
		t.Fatal("copy silently omitted recorded archive authority")
	}
	asset := version
	if !mysql {
		asset = "sqlite/0064_traffic_record_meta_archives.sql"
	}
	ddl, err := migrations.Files.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&header).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err != nil {
		t.Fatal("explicit owned archive restoration", err)
	}
	if err := source.Exec("ALTER TABLE " + table + " DROP COLUMN fact_sha256").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil {
		t.Fatal("partial proof archive silently adopted")
	}
	var retained int64
	if err := source.Model(&model.TrafficRecord{}).Where("id = ?", fact.ID).Count(&retained).Error; err != nil || retained != 1 {
		t.Fatal("schema refusal changed billing fact", retained, err)
	}
}
