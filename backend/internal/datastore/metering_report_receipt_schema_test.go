package datastore_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestSQLiteMeteringReportReceiptMigrationRecoveryAndCopy(t *testing.T) {
	assertReportReceiptMigration(t, false)
}
func TestMySQLMeteringReportReceiptMigrationRecoveryAndCopy(t *testing.T) {
	assertReportReceiptMigration(t, true)
}
func assertReportReceiptMigration(t *testing.T, mysql bool) {
	source := diagnosticSchemaDB(t, mysql)
	fact := diagnosticSchemaSource(t, source, false)
	const version = "0063_metering_report_receipts.up.sql"
	const table = "metering_report_receipts"
	receipt := model.MeteringReportReceipt{NodeID: fact.NodeID, ReportID: fact.ReportID, ProtocolEndpointID: fact.ProtocolEndpointID, TrafficRecordID: fact.ID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := source.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	var original model.TrafficRecord
	if err := source.First(&original, fact.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalJSON, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var versions int64
	if err := source.Table("schema_migrations").Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	assertPreserved := func() {
		t.Helper()
		var current model.TrafficRecord
		if err := source.First(&current, fact.ID).Error; err != nil {
			t.Fatal(err)
		}
		currentJSON, err := json.Marshal(current)
		if err != nil || string(currentJSON) != string(originalJSON) || current.Nonce != original.Nonce {
			t.Fatal("migration changed original billing or diagnostic source", err)
		}
		var count int64
		if err := source.Table("schema_migrations").Count(&count).Error; err != nil || count != versions {
			t.Fatal("migration history changed during refusal/repeat", err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(source); err != nil {
			t.Fatal(err)
		}
		assertPreserved()
	}
	// A recorded loss is not a fresh installation. Preserve it as a failure
	// until the exact owned backup is restored; never manufacture empty identity.
	if err := source.Exec("DROP TABLE " + table).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil || !strings.Contains(err.Error(), "identities missing") {
		t.Fatal("recorded identity loss silently repaired", err)
	}
	if source.Migrator().HasTable(table) {
		t.Fatal("refusal created an empty authority table")
	}
	assertPreserved()
	asset := version
	if !mysql {
		asset = "sqlite/0063_metering_report_receipts.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Exec(string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err != nil {
		t.Fatal("explicit backup restoration", err)
	}
	assertPreserved()
	if err := source.Exec("ALTER TABLE " + table + " DROP COLUMN protocol_endpoint_id").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil {
		t.Fatal("recorded identity column loss silently repaired")
	}
	if source.Migrator().HasColumn(table, "protocol_endpoint_id") {
		t.Fatal("refusal invented identity field")
	}
	assertPreserved()
	kind := "integer"
	if mysql {
		kind = "bigint unsigned"
	}
	if err := source.Exec("ALTER TABLE " + table + " ADD COLUMN protocol_endpoint_id " + kind + " NOT NULL DEFAULT 0").Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Table(table).Where("node_id = ? AND report_id = ?", receipt.NodeID, receipt.ReportID).Update("protocol_endpoint_id", receipt.ProtocolEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err != nil {
		t.Fatal("explicit field restoration", err)
	}
	assertPreserved()
	target := diagnosticSchemaDB(t, mysql)
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
		t.Fatal("copy receipt with authoritative data", err)
	}
	var copied model.MeteringReportReceipt
	if err := target.Where("node_id = ? AND report_id = ?", receipt.NodeID, receipt.ReportID).First(&copied).Error; err != nil || copied.TrafficRecordID != receipt.TrafficRecordID || copied.ProtocolEndpointID != receipt.ProtocolEndpointID || !copied.CreatedAt.Equal(receipt.CreatedAt) {
		t.Fatalf("copy lost replay identity %+v %v", copied, err)
	}
	var copiedFact model.TrafficRecord
	if err := target.First(&copiedFact, fact.ID).Error; err != nil || copiedFact.UsedBytes != fact.UsedBytes || copiedFact.RawBytes != fact.RawBytes || copiedFact.Meta != fact.Meta || copiedFact.Nonce != fact.Nonce {
		t.Fatal("copy changed source accounting/proof", err)
	}
}
