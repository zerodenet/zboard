package datastore_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSQLiteAuditColumnBatchPreservesRecovery(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "audit-columns.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	assertAuditColumnBatchRecovery(t, db)
}

func TestMySQLAuditColumnBatchPreservesRecovery(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	assertAuditColumnBatchRecovery(t, db)
}

type auditColumnContract struct {
	DataType, FullType, Default string
	Length                      int64
	Nullable, HasLength         bool
	HasDefault                  bool
	PrimaryKey                  bool
}

var auditColumnBatchFields = map[string][]string{
	"plugin_operations":     {"previous_publisher", "publisher", "signing_key_fingerprint", "package_digest"},
	"node_config_publishes": {"phase", "applied_config_sha256"},
}

func auditColumnTableAlter(query, table string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return strings.HasPrefix(query, "alter table `"+table+"` ") || strings.HasPrefix(query, "alter table "+table+" ")
}

func auditColumnTargetAddition(query, table string) bool {
	if !auditColumnTableAlter(query, table) {
		return false
	}
	query = strings.ToLower(query)
	for _, column := range auditColumnBatchFields[table] {
		for _, prefix := range []string{"add ", "add column "} {
			if strings.Contains(query, prefix+"`"+column+"` ") || strings.Contains(query, prefix+column+" ") {
				return true
			}
		}
	}
	return false
}

func auditColumnContracts(t *testing.T, db *gorm.DB) map[string]map[string]auditColumnContract {
	t.Helper()
	result := map[string]map[string]auditColumnContract{}
	for _, table := range []string{"plugin_operations", "node_config_publishes"} {
		result[table] = map[string]auditColumnContract{}
		if db.Dialector.Name() == "sqlite" {
			// ColumnTypes supplements declarations with driver metadata whose
			// TEXT/DATETIME lengths change when the table gains its first row.
			// Compare the actual persisted declaration instead.
			var columns []struct {
				Name    string
				Type    string
				NotNull int     `gorm:"column:notnull"`
				Default *string `gorm:"column:dflt_value"`
				PK      int     `gorm:"column:pk"`
			}
			if err := db.Raw("PRAGMA table_info(?)", clause.Table{Name: table}).Scan(&columns).Error; err != nil {
				t.Fatal(err)
			}
			for _, column := range columns {
				contract := auditColumnContract{DataType: strings.ToLower(column.Type), FullType: strings.ToLower(column.Type), Nullable: column.NotNull == 0, HasDefault: column.Default != nil, PrimaryKey: column.PK != 0}
				if column.Default != nil {
					contract.Default = *column.Default
				}
				result[table][column.Name] = contract
			}
			continue
		}
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			fullType, _ := column.ColumnType()
			length, hasLength := column.Length()
			nullable, _ := column.Nullable()
			defaultValue, hasDefault := column.DefaultValue()
			primaryKey, _ := column.PrimaryKey()
			result[table][column.Name()] = auditColumnContract{DataType: column.DatabaseTypeName(), FullType: fullType, Length: length, HasLength: hasLength, Nullable: nullable, Default: defaultValue, HasDefault: hasDefault, PrimaryKey: primaryKey}
		}
	}
	return result
}

func auditStableRows(t *testing.T, db *gorm.DB) map[string][]map[string]any {
	t.Helper()
	result := map[string][]map[string]any{}
	for table, columns := range map[string]string{
		"plugin_operations": "id, plugin_id, action, state, actor, message, created_at, updated_at",
		// Existing phase reconciliation updates updated_at when it resets a
		// connections row with missing SHA. All other historical fields stay.
		"node_config_publishes": "node_id, endpoint_id, requested_by, generation, attempts, reconciling, suspended, deferred, next_attempt_at, lease_until, lease_token, last_error, created_at",
	} {
		var rows []map[string]any
		if err := db.Table(table).Select(columns).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		result[table] = rows
	}
	return result
}

func assertAuditColumnBatchRecovery(t *testing.T, db *gorm.DB) {
	t.Helper()
	isMySQL := db.Dialector.Name() == "mysql"
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	var observed []string
	const callback = "test:audit_column_batch"
	if err := db.Callback().Raw().After("gorm:raw").Register(callback, func(tx *gorm.DB) {
		if tx.Error == nil {
			observed = append(observed, strings.TrimSpace(tx.Statement.SQL.String()))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Raw().Remove(callback) })
	wantAlters := func(missing int) int {
		if isMySQL && missing > 0 {
			return 1
		}
		return missing
	}
	run := func(label string, call func() error, pluginMissing, phaseMissing int, isolated bool) {
		t.Helper()
		observed = nil
		var beforeID, beforeAlter uint64
		if isMySQL {
			beforeID, beforeAlter = mysqlSessionAlterCount(t, pool)
		}
		if err := call(); err != nil {
			t.Fatal(label, err)
		}
		counts := map[string]int{}
		allTableAlters := map[string]int{}
		relatedDDL := map[string]map[string]int{}
		relatedSQLHashes := map[string]int{}
		for _, query := range observed {
			kind := ""
			for _, prefix := range []string{"ALTER TABLE ", "CREATE TABLE ", "DROP TABLE "} {
				if strings.HasPrefix(strings.ToUpper(query), prefix) {
					kind = strings.TrimSpace(prefix)
				}
			}
			for _, table := range []string{"plugin_operations", "node_config_publishes"} {
				// Keep SQLite's temporary-table rebuild/rename work visible,
				// separately from exact-table ALTER and the owned column ADDs.
				if kind != "" && strings.Contains(query, table) {
					if relatedDDL[table] == nil {
						relatedDDL[table] = map[string]int{}
					}
					relatedDDL[table][kind]++
					relatedSQLHashes[fmt.Sprintf("%s/%s/%x", table, kind, sha256.Sum256([]byte(query)))]++
				}
				if auditColumnTableAlter(query, table) {
					allTableAlters[table]++
				}
				if auditColumnTargetAddition(query, table) {
					counts[table]++
				}
			}
		}
		wantPlugin, wantPhase := wantAlters(pluginMissing), wantAlters(phaseMissing)
		t.Logf("%s related table/temp DDL=%v SQL SHA256 counts=%v", label, relatedDDL, relatedSQLHashes)
		if counts["plugin_operations"] != wantPlugin || counts["node_config_publishes"] != wantPhase {
			// Nonfatal so an old-callsite overlay can still check preservation
			// and fail specifically on the real four/two ALTER count.
			t.Errorf("%s: observed target ADD ALTER per table=%v, want plugin=%d phase=%d (all exact-table ALTER=%v)", label, counts, wantPlugin, wantPhase, allTableAlters)
		}
		if isMySQL {
			afterID, afterAlter := mysqlSessionAlterCount(t, pool)
			if beforeID != afterID || afterAlter < beforeAlter {
				t.Fatal(label, "session counter lost its connection")
			}
			if isolated && afterAlter-beforeAlter != uint64(wantPlugin+wantPhase) {
				t.Errorf("%s: actual Com_alter_table delta=%d, want %d", label, afterAlter-beforeAlter, wantPlugin+wantPhase)
			}
			t.Logf("%s session=%d Com_alter_table delta=%d target ADD ALTER=%v all exact-table ALTER=%v", label, beforeID, afterAlter-beforeAlter, counts, allTableAlters)
		} else {
			t.Logf("%s SQLite target ADD ALTER=%v all exact-table ALTER=%v", label, counts, allTableAlters)
		}
	}
	// MySQL's published tables lack both groups. SQLite's plugin table comes
	// from its original 0002 SQL (four missing audit fields), while SQLite
	// AutoMigrate already creates the publication model's current phase fields.
	// Neither path replaces RunMigrations or its authority checks.
	freshPluginMissing, freshPhaseMissing := 4, 0
	if isMySQL {
		freshPhaseMissing = 2
	}
	run("fresh actual RunMigrations", func() error { return datastore.RunMigrations(db) }, freshPluginMissing, freshPhaseMissing, false)
	wantContract := auditColumnContracts(t, db)
	for table, lengths := range map[string]map[string]int64{
		"plugin_operations":     {"previous_publisher": 160, "publisher": 160, "signing_key_fingerprint": 64, "package_digest": 64},
		"node_config_publishes": {"phase": 20, "applied_config_sha256": 64},
	} {
		for name, length := range lengths {
			column, ok := wantContract[table][name]
			emptyDefault := column.Default == "" || (!isMySQL && column.Default == "''")
			if !ok || column.Nullable || !column.HasDefault || !emptyDefault || column.PrimaryKey || (isMySQL && (!column.HasLength || column.Length != length)) {
				t.Fatalf("fresh contract %s.%s: %+v", table, name, column)
			}
		}
	}
	now := time.Date(2026, 10, 20, 2, 3, 4, 0, time.UTC)
	node := model.Node{ID: 4, Name: "audit-columns", Address: "localhost", Config: "{}"}
	operation := model.PluginOperation{ID: "retained-audit", PluginID: "test.audit", Action: "update", State: "succeeded", Actor: "retained-actor", Message: "retained-message", PreviousPublisher: "old.publisher", Publisher: "new.publisher", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), CreatedAt: now, UpdatedAt: now}
	publication := model.NodeConfigPublish{NodeID: node.ID, EndpointID: 7, RequestedBy: 9, Generation: 11, Attempts: 13, Reconciling: true, Suspended: true, Deferred: true, NextAttemptAt: now, LeaseUntil: now, LeaseToken: "retained-lease", LastError: "retained-error", Phase: "configuration", AppliedConfigSHA256: strings.Repeat("c", 64), CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&node, &operation, &publication} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	wantRows := auditStableRows(t, db)
	check := func() {
		t.Helper()
		if got := auditStableRows(t, db); !reflect.DeepEqual(got, wantRows) {
			t.Fatal("historical non-target fields changed", got, wantRows)
		}
		if got := auditColumnContracts(t, db); !reflect.DeepEqual(got, wantContract) {
			t.Fatal("column definitions changed", got, wantContract)
		}
	}
	drop := func(value any, fields ...string) {
		t.Helper()
		for _, field := range fields {
			if err := db.Migrator().DropColumn(value, field); err != nil {
				t.Fatal(err)
			}
		}
	}
	const version = "0045_publication_connection_phase.up.sql"
	forgetPhase := func() {
		t.Helper()
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
			t.Fatal(err)
		}
	}
	both := func() error {
		if err := datastore.ReconcilePluginOperationColumnsForTest(db); err != nil {
			return err
		}
		return datastore.ReconcilePublicationPhaseColumnsForTest(db)
	}
	drop(&model.PluginOperation{}, "PreviousPublisher", "Publisher", "SigningKeyFingerprint", "PackageDigest")
	drop(&model.NodeConfigPublish{}, "Phase", "AppliedConfigSHA256")
	forgetPhase()
	run("all missing owned recovery", both, 4, 2, true)
	check()
	var savedOperation model.PluginOperation
	var savedPublication model.NodeConfigPublish
	if err := db.First(&savedOperation, "id = ?", operation.ID).Error; err != nil || savedOperation.PreviousPublisher != "" || savedOperation.Publisher != "" || savedOperation.SigningKeyFingerprint != "" || savedOperation.PackageDigest != "" {
		t.Fatal("missing audit evidence manufactured", savedOperation, err)
	}
	if err := db.First(&savedPublication, "node_id = ?", node.ID).Error; err != nil || savedPublication.Phase != "" || savedPublication.AppliedConfigSHA256 != "" {
		t.Fatal("missing publication evidence manufactured", savedPublication, err)
	}
	run("complete repeat", both, 0, 0, true)
	// Simulate a committed DDL prefix before the 0045 receipt: existing audit
	// publisher bytes survive, and missing SHA cannot close a connections phase.
	if err := db.Model(&operation).Updates(map[string]any{"previous_publisher": "retained.previous", "publisher": "retained.publisher"}).Error; err != nil {
		t.Fatal(err)
	}
	// This fixture update is intentional, so refresh the immutable-row baseline.
	wantRows = auditStableRows(t, db)
	if err := db.Model(&publication).Update("phase", "connections").Error; err != nil {
		t.Fatal(err)
	}
	drop(&model.PluginOperation{}, "SigningKeyFingerprint", "PackageDigest")
	drop(&model.NodeConfigPublish{}, "AppliedConfigSHA256")
	forgetPhase()
	run("partial actual RunMigrations", func() error { return datastore.RunMigrations(db) }, 2, 1, false)
	check()
	if err := db.First(&savedOperation, "id = ?", operation.ID).Error; err != nil || savedOperation.PreviousPublisher != "retained.previous" || savedOperation.Publisher != "retained.publisher" {
		t.Fatal("committed audit prefix changed", savedOperation, err)
	}
	if err := db.First(&savedPublication, "node_id = ?", node.ID).Error; err != nil || savedPublication.Phase != "" || savedPublication.AppliedConfigSHA256 != "" {
		t.Fatal("missing SHA treated as applied", savedPublication, err)
	}
	forgetPhase()
	run("DDL complete receipt missing", both, 0, 0, true)
	// Inject a real DDL-call failure before gorm:raw. Metadata remains real.
	// Recovery must not issue the phase UPDATE or record 0045 after that error.
	drop(&model.NodeConfigPublish{}, "AppliedConfigSHA256")
	forgetPhase()
	denied := errors.New("injected publication ALTER failure")
	const failCallback = "test:fail_publication_column_batch"
	if err := db.Callback().Raw().Before("gorm:raw").Register(failCallback, func(tx *gorm.DB) {
		query := tx.Statement.SQL.String()
		if auditColumnTargetAddition(query, "node_config_publishes") {
			tx.AddError(denied)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err = datastore.RunMigrations(db)
	if err := db.Callback().Raw().Remove(failCallback); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, denied) {
		t.Fatal("failed ALTER lost its error", err)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed DDL recorded 0045", count, err)
	}
	if got := auditStableRows(t, db); !reflect.DeepEqual(got, wantRows) {
		t.Fatal("failed DDL changed existing history")
	}
	run("failed ALTER retry", both, 0, 1, true)
	check()
}
