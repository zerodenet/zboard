package datastore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

const diagnosticVersion = "0062_traffic_diagnostic_material_retention.up.sql"
const diagnosticTable = "traffic_diagnostic_materials"
const diagnosticIndex = "idx_traffic_diagnostic_review"

func diagnosticSchemaDB(t *testing.T, mysql bool) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	if mysql {
		db, _ = hourlyMySQLFixture(t)
	} else {
		var err error
		db, err = datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "diagnostic-schema.db"))
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func diagnosticSchemaSource(t *testing.T, db *gorm.DB, proven bool) model.TrafficRecord {
	t.Helper()
	// Respect the published MySQL source FKs rather than using zero IDs that
	// happen to work in SQLite's model-created legacy table.
	user := model.User{AccountName: "diagnostic-schema", Email: "diagnostic-schema@example.test", Password: "unused-fixture-value", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "diagnostic-schema-node", Region: "test", Address: "127.0.0.1", AssetTag: strings.Repeat("a", 64), Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "diagnostic-schema-endpoint", Protocol: "socks5", Address: "127.0.0.1", ListenAddress: "127.0.0.1", Port: 1080, PublicPort: 1080, RuntimeKey: "10000000-0000-4000-8000-000000000001", MultiplierMilli: 1500, OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	row := model.TrafficRecord{UserID: user.ID, NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: "diagnostic-schema", Nonce: "diagnostic-schema", RawBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: time.Now().UTC(), Meta: strings.Repeat("schema source", 1000)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if proven {
		sum := sha256.Sum256([]byte(row.Meta))
		now := time.Now().UTC().Truncate(time.Millisecond)
		if err := db.Create(&model.TrafficDiagnosticMaterial{TrafficRecordID: row.ID, SourceBytes: uint64(len(row.Meta)), SourceSHA256: hex.EncodeToString(sum[:]), CreatedAt: now, ReviewAt: now.Add(7 * 24 * time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return row
}

func diagnosticSchemaDDL(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	asset := diagnosticVersion
	if datastore.IsSQLite(db) {
		asset = "sqlite/0062_traffic_diagnostic_material_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for _, s := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(s) != "" {
			statements = append(statements, strings.TrimSpace(s))
		}
	}
	if len(statements) != 2 {
		t.Fatal("formal migration statements changed", len(statements))
	}
	return statements
}

func TestSQLiteDiagnosticMaterialMigrationRecoveryAndCopy(t *testing.T) {
	assertDiagnosticSchema(t, false)
}
func TestMySQLDiagnosticMaterialMigrationRecoveryAndCopy(t *testing.T) {
	assertDiagnosticSchema(t, true)
}

func assertDiagnosticSchema(t *testing.T, mysql bool) {
	t.Helper()
	t.Run("old upgrade remains unclassified", func(t *testing.T) {
		db := diagnosticSchemaDB(t, mysql)
		row := diagnosticSchemaSource(t, db, false)
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", diagnosticVersion).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropTable(diagnosticTable); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := datastore.RunMigrations(db); err != nil {
				t.Fatal("old/repeated upgrade", err)
			}
		}
		var got model.TrafficRecord
		var n int64
		if err := db.First(&got, row.ID).Error; err != nil || got.Meta != row.Meta {
			t.Fatal("upgrade rewrote old material", err)
		}
		if err := db.Table(diagnosticTable).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("historical classification fabricated", n, err)
		}
	})
	for _, completeDDL := range []bool{false, true} {
		name := "interrupted DDL"
		if completeDDL {
			name = "formal DDL before receipt"
		}
		t.Run(name, func(t *testing.T) {
			db := diagnosticSchemaDB(t, mysql)
			if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", diagnosticVersion).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Migrator().DropTable(diagnosticTable); err != nil {
				t.Fatal(err)
			}
			ddl := diagnosticSchemaDDL(t, db)
			if err := db.Exec(ddl[0]).Error; err != nil {
				t.Fatal(err)
			}
			if completeDDL {
				if err := db.Exec(ddl[1]).Error; err != nil {
					t.Fatal(err)
				}
			}
			source := diagnosticSchemaSource(t, db, true)
			if err := datastore.RunMigrations(db); err != nil {
				t.Fatal("resume", err)
			}
			if !db.Migrator().HasIndex(diagnosticTable, diagnosticIndex) {
				t.Fatal("bounded review index absent")
			}
			var n int64
			if err := db.Table("schema_migrations").Where("version = ?", diagnosticVersion).Count(&n).Error; err != nil || n != 1 {
				t.Fatal("migration receipt", n, err)
			}
			var header model.TrafficDiagnosticMaterial
			if err := db.First(&header, "traffic_record_id = ?", source.ID).Error; err != nil || header.RetiredAt != nil || header.SourceBytes != uint64(len(source.Meta)) {
				t.Fatal("resume discarded provenance", err)
			}
		})
	}
	for _, fault := range []string{"lost table", "lost column", "wrong foreign key", "wrong index"} {
		t.Run(fault, func(t *testing.T) {
			db := diagnosticSchemaDB(t, mysql)
			source := diagnosticSchemaSource(t, db, fault != "wrong foreign key")
			switch fault {
			case "lost table":
				if err := db.Migrator().DropTable(diagnosticTable); err != nil {
					t.Fatal(err)
				}
			case "lost column":
				if err := db.Migrator().DropColumn(diagnosticTable, "source_sha256"); err != nil {
					t.Fatal(err)
				}
			case "wrong foreign key":
				if err := db.Migrator().DropTable(diagnosticTable); err != nil {
					t.Fatal(err)
				}
				ddl := strings.Replace(diagnosticSchemaDDL(t, db)[0], "ON DELETE RESTRICT", "ON DELETE CASCADE", 1)
				if err := db.Exec(ddl).Error; err != nil {
					t.Fatal(err)
				}
			case "wrong index":
				if err := db.Migrator().DropIndex(diagnosticTable, diagnosticIndex); err != nil {
					t.Fatal(err)
				}
				if err := db.Exec("CREATE INDEX " + diagnosticIndex + " ON " + diagnosticTable + "(traffic_record_id)").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := datastore.RunMigrations(db); err == nil {
				t.Fatal("recorded provenance/index fault silently repaired")
			}
			var got model.TrafficRecord
			if err := db.First(&got, source.ID).Error; err != nil || got.Meta != source.Meta || got.UsedBytes != source.UsedBytes {
				t.Fatal("failed startup changed source facts", err)
			}
			if fault == "lost table" && db.Migrator().HasTable(diagnosticTable) {
				t.Fatal("lost evidence recreated as empty")
			}
			if fault != "wrong index" {
				target := diagnosticSchemaDB(t, mysql)
				if err := datastore.CopyApplicationData(db, target); err == nil || !strings.Contains(err.Error(), "diagnostic") {
					t.Fatal("copy accepted unproven source provenance", err)
				}
			}
		})
	}
	t.Run("retired receipt copy", func(t *testing.T) {
		source := diagnosticSchemaDB(t, mysql)
		row := diagnosticSchemaSource(t, source, true)
		now := time.Now().UTC().Add(8 * 24 * time.Hour)
		if out, err := (meteringstore.DiagnosticMaterialRetention{DB: source, Now: func() time.Time { return now }}).Retire(t.Context(), 32, meteringstore.DiagnosticMaterialReadBudget); err != nil || out.Retired != 1 {
			t.Fatal("prepare real retirement", out, err)
		}
		var before model.TrafficRecord
		var original model.TrafficDiagnosticMaterial
		if err := source.First(&before, row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := source.First(&original, "traffic_record_id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		target := diagnosticSchemaDB(t, mysql)
		tables, err := datastore.MigrationTables(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := target.Transaction(func(tx *gorm.DB) error {
			for i := len(tables) - 1; i >= 0; i-- {
				if tx.Migrator().HasTable(tables[i]) {
					if err := tx.Exec("DELETE FROM " + tables[i]).Error; err != nil {
						return err
					}
				}
			}
			return datastore.CopyApplicationData(source, tx)
		}); err != nil {
			t.Fatal("copy", err)
		}
		var after model.TrafficRecord
		var copied model.TrafficDiagnosticMaterial
		if err := target.First(&after, row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := target.First(&copied, "traffic_record_id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		encode := func(r model.TrafficRecord) string {
			b, err := json.Marshal(struct {
				Record model.TrafficRecord
				Nonce  string
				Epoch  uint64
			}{r, r.Nonce, r.EntitlementEpoch})
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		if encode(before) != encode(after) || original.TrafficRecordID != copied.TrafficRecordID || original.SourceBytes != copied.SourceBytes || original.SourceSHA256 != copied.SourceSHA256 || original.RetiredAt == nil || copied.RetiredAt == nil || !original.RetiredAt.Equal(*copied.RetiredAt) || !original.CreatedAt.Equal(copied.CreatedAt) || !original.ReviewAt.Equal(copied.ReviewAt) {
			t.Fatal("copy lost immutable facts/original-material retirement receipt")
		}
		if out, err := (meteringstore.DiagnosticMaterialRetention{DB: target, Now: func() time.Time { return now }}).Retire(t.Context(), 32, meteringstore.DiagnosticMaterialReadBudget); err != nil || out.Retired != 0 {
			t.Fatal("copied receipt retired again", out, err)
		}
	})
}
