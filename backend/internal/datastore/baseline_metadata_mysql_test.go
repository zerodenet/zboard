package datastore_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMySQLBaselineMetadataBatchRetainsLegacyValidationAndGuards(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	// The fixture always traverses the complete real preparation boundary.
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	marker := model.Node{ID: 4100, Name: "baseline-metadata-retained", Address: "localhost", Config: `{"retained":true}`}
	if err := db.Create(&marker).Error; err != nil {
		t.Fatal(err)
	}
	var beforeNode model.Node
	if err := db.First(&beforeNode, marker.ID).Error; err != nil {
		t.Fatal(err)
	}
	type migrationRow struct {
		Version   string
		AppliedAt time.Time
	}
	var history []migrationRow
	if err := db.Table("schema_migrations").Order("version").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	status := func() (uint64, uint64, uint64) {
		id, alters := mysqlSessionAlterCount(t, pool)
		var variable string
		var selects uint64
		if err := pool.QueryRow("SHOW SESSION STATUS LIKE 'Com_select'").Scan(&variable, &selects); err != nil {
			t.Fatal(err)
		}
		return id, alters, selects
	}
	beforeID, beforeAlter, beforeSelect := status()
	if err := datastore.ValidatePreReleaseBaselineSchemaForTest(pool); err != nil {
		t.Fatal("valid prepared baseline rejected", err)
	}
	afterID, afterAlter, afterSelect := status()
	// The trailing status() contributes exactly one SELECT CONNECTION_ID();
	// SHOW SESSION STATUS contributes no Com_select increment.
	if beforeID != afterID || afterAlter != beforeAlter || afterSelect < beforeSelect+1 || afterSelect-beforeSelect-1 != 3 {
		t.Errorf("isolated validator query count changed: id=%d→%d ALTER=%d→%d Com_select=%d→%d (want3 validation SELECTs plus1 ID probe)", beforeID, afterID, beforeAlter, afterAlter, beforeSelect, afterSelect)
	}
	t.Logf("same MySQL session %d: validator SELECTs=%d, ALTER=%d", beforeID, afterSelect-beforeSelect-1, afterAlter-beforeAlter)
	assertRejected := func(want string) {
		t.Helper()
		if err := datastore.ValidatePreReleaseBaselineSchemaForTest(pool); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("invalid baseline accepted or diagnostic changed: want %q, got %v", want, err)
		}
	}
	var plans int64
	if err := db.Table("plans").Count(&plans).Error; err != nil || plans != 0 {
		t.Fatal("schema mutation fixture requires an empty plans table", plans, err)
	}
	if err := db.Exec("ALTER TABLE plans DROP COLUMN revision").Error; err != nil {
		t.Fatal(err)
	}
	assertRejected("missing column plans.revision")
	if err := db.Exec("ALTER TABLE plans ADD COLUMN revision bigint NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatal(err)
	}
	assertRejected("plans.revision has type bigint, want bigint unsigned")
	if err := db.Exec("ALTER TABLE plans MODIFY COLUMN revision bigint unsigned NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE plans RENAME COLUMN revision TO REVISION").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.ValidatePreReleaseBaselineSchemaForTest(pool); err != nil {
		t.Fatal("case-insensitive legacy column identity rejected", err)
	}
	if err := db.Exec("ALTER TABLE plans RENAME COLUMN REVISION TO revision").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_audit_logs_history_cursor ON audit_logs").Error; err != nil {
		t.Fatal(err)
	}
	assertRejected("missing index audit_logs.idx_audit_logs_history_cursor")
	if err := db.Exec("CREATE INDEX idx_audit_logs_history_cursor ON audit_logs(created_at,id)").Error; err != nil {
		t.Fatal(err)
	}
	// Older baselines did not include the later renewal_effect fields. Their
	// absence must not become a new requirement of baseline adoption.
	for _, table := range []string{"orders", "plan_skus"} {
		if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN renewal_effect").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.ValidatePreReleaseBaselineSchemaForTest(pool); err != nil {
		t.Fatal("valid legacy baseline shape rejected", err)
	}
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal("legacy-compatible shape failed full preparation", err)
	}
	const unknown = "legacy-metadata-unknown.up.sql"
	if err := db.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)", unknown, time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "must reach 0032") {
		t.Fatal("unknown old migration chain was adopted", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "unversioned pre-release database cannot adopt") {
		t.Fatal("unversioned populated schema was adopted", err)
	}
	if err := db.Table("schema_migrations").CreateInBatches(&history, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal("restored migration history failed preparation", err)
	}
	var afterNode model.Node
	if err := db.First(&afterNode, marker.ID).Error; err != nil || !reflect.DeepEqual(afterNode, beforeNode) {
		t.Fatal("metadata validation changed source data", err)
	}
	var afterHistory []migrationRow
	if err := db.Table("schema_migrations").Order("version").Find(&afterHistory).Error; err != nil || !reflect.DeepEqual(afterHistory, history) {
		t.Fatal("metadata validation changed migration history", err)
	}
}
