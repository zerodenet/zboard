package datastore_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func TestMySQLHourlyProjectionBatchesMissingColumnsWithoutChangingCutover(t *testing.T) {
	db, admin := hourlyMySQLFixture(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	const table = "metering_usage_projection_pending"
	const version = "0054_hourly_usage_projection.up.sql"
	columns := []string{"hourly_pending", "user_id", "subscription_id", "node_id", "record_at"}
	indexes := []string{"idx_usage_hourly_pending_time", "idx_usage_hourly_pending_user", "idx_usage_hourly_pending_subscription", "idx_usage_hourly_pending_node", "idx_usage_hourly_pending_endpoint"}
	payload, err := migrations.Files.ReadFile(version)
	if err != nil {
		t.Fatal(err)
	}
	// These published statements are used only to simulate already committed
	// prefixes. Production recovery still goes through the real RunMigrations.
	var ddlLines []string
	for _, line := range strings.Split(string(payload), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			ddlLines = append(ddlLines, line)
		}
	}
	statements := strings.Split(strings.Join(ddlLines, "\n"), ";")
	var observed []string
	if err := db.Callback().Raw().After("gorm:raw").Register("test:hourly_column_batch", func(tx *gorm.DB) {
		if tx.Error == nil {
			observed = append(observed, strings.TrimSpace(tx.Statement.SQL.String()))
		}
	}); err != nil {
		t.Fatal(err)
	}
	counters := func() (uint64, uint64, uint64) {
		id, alters := mysqlSessionAlterCount(t, pool)
		var variable string
		var creates uint64
		if err := pool.QueryRow("SHOW SESSION STATUS LIKE 'Com_create_index'").Scan(&variable, &creates); err != nil {
			t.Fatal(err)
		}
		return id, alters, creates
	}
	run := func(missingColumns, missingIndexes int) {
		t.Helper()
		observed = nil
		beforeID, beforeAlter, beforeCreate := counters()
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("real migration/recovery boundary", err)
		}
		afterID, afterAlter, afterCreate := counters()
		if beforeID != afterID || afterAlter < beforeAlter || afterCreate < beforeCreate {
			t.Fatal("DDL counters lost their MySQL session")
		}
		var additions []string
		created, dropped := 0, 0
		for _, statement := range observed {
			if strings.HasPrefix(statement, "ALTER TABLE "+table+" ADD COLUMN ") {
				additions = append(additions, statement)
			}
			if strings.HasPrefix(statement, "CREATE INDEX idx_usage_hourly_pending_") {
				created++
			}
			if strings.HasPrefix(statement, "DROP TRIGGER IF EXISTS traffic_hourly_") {
				dropped++
			}
		}
		wantStatements := 0
		if missingColumns > 0 {
			wantStatements = 1
		}
		if len(additions) != wantStatements || strings.Count(strings.Join(additions, ","), "ADD COLUMN ") != missingColumns || created != missingIndexes || dropped < 4 {
			t.Errorf("cutover DDL mismatch: columns=%d statements=%d want=%d indexes=%d want=%d explicit trigger drops=%d", missingColumns, len(additions), wantStatements, created, missingIndexes, dropped)
		}
		t.Logf("same session %d: missing columns=%d, column ALTER statements=%d, Com_alter_table delta=%d, Com_create_index delta=%d", beforeID, missingColumns, len(additions), afterAlter-beforeAlter, afterCreate-beforeCreate)
	}
	run(5, 5)
	run(0, 0)
	for _, parent := range []any{
		&model.User{ID: 2, Email: "hourly-batch@example.test", Password: "unused", Status: "active"},
		&model.Node{ID: 4, Name: "hourly-batch", Address: "localhost", Config: "{}"},
		&model.ProtocolEndpoint{ID: 5, NodeID: 4, Name: "hourly-batch", Protocol: "vless", ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000},
	} {
		if err := db.Create(parent).Error; err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	fact := model.TrafficRecord{UserID: 2, SubscriptionID: 3, NodeID: 4, ProtocolEndpointID: 5, ReportID: "legacy-batch", Nonce: "legacy-batch", At: at, RawBytes: 7, UploadBytes: 3, DownloadBytes: 4, UsedBytes: 11, ProtocolMultiplierMilli: 1000, Meta: `{"retained":"legacy source"}`}
	if err := db.Create(&fact).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(table).Create(map[string]any{"traffic_record_id": fact.ID, "protocol_endpoint_id": fact.ProtocolEndpointID, "created_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	reset := func(committedColumns, committedIndexes int) {
		t.Helper()
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
			t.Fatal(err)
		}
		for _, index := range indexes {
			if err := db.Exec("DROP INDEX " + index + " ON " + table).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, column := range columns {
			if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, statement := range statements[:committedColumns] {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal("simulate committed column prefix", err)
			}
		}
		for _, statement := range statements[5 : 5+committedIndexes] {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal("simulate committed indexes", err)
			}
		}
	}
	for _, prefix := range []struct{ columns, indexes int }{{0, 0}, {2, 0}, {5, 2}} {
		reset(prefix.columns, prefix.indexes)
		run(5-prefix.columns, 5-prefix.indexes)
		run(0, 0)
		var pending model.MeteringUsageProjectionPending
		if err := db.First(&pending, "traffic_record_id = ?", fact.ID).Error; err != nil || pending.HourlyPending || pending.RecordAt != nil || pending.ProtocolEndpointID != fact.ProtocolEndpointID || !pending.CreatedAt.Equal(at) {
			t.Fatal("legacy completion evidence changed", pending, err)
		}
		assertHourlyTotals(t, db, 1, 7, 3, 4, 11)
	}
	// Denied ADD COLUMN must not manufacture receipt or completion evidence.
	reset(0, 0)
	var identity struct{ Schema, Account string }
	if err := db.Raw("SELECT DATABASE() AS `schema`, SUBSTRING_INDEX(CURRENT_USER(),'@',1) AS account").Scan(&identity).Error; err != nil {
		t.Fatal(err)
	}
	grant := "GRANT ALTER ON `" + identity.Schema + "`.* TO '" + identity.Account + "'@'%'"
	t.Cleanup(func() { _, _ = admin.Exec(grant) })
	if _, err := admin.Exec("REVOKE ALTER ON `" + identity.Schema + "`.* FROM '" + identity.Account + "'@'%'"); err != nil {
		t.Fatal(err)
	}
	pool.SetMaxIdleConns(0)
	pool.SetMaxIdleConns(1)
	err = datastore.RunMigrations(db)
	var denied *mysqldriver.MySQLError
	if !errors.As(err, &denied) || denied.Number != 1142 {
		t.Fatalf("missing ALTER privilege was not propagated: %v", err)
	}
	var receipts int64
	if err := db.Table("schema_migrations").Where("version = ?", version).Count(&receipts).Error; err != nil || receipts != 0 || db.Migrator().HasColumn(table, "hourly_pending") {
		t.Fatal("denied DDL changed recovery evidence", receipts, err)
	}
	if _, err := admin.Exec(grant); err != nil {
		t.Fatal(err)
	}
	pool.SetMaxIdleConns(0)
	pool.SetMaxIdleConns(1)
	run(5, 5)
	assertHourlyBatchDefinition(t, db)
	worker := meteringstore.UsageProjectionWorker{DB: db}
	if count, err := worker.Drain(t.Context(), 200); err != nil || count != 1 {
		t.Fatal("legacy daily-only recovery", count, err)
	}
	assertHourlyTotals(t, db, 1, 7, 3, 4, 11)
	current := fact
	current.ID, current.ReportID, current.Nonce, current.UsedBytes = 0, "current-batch", "current-batch", 13
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&current).Error; err != nil {
			return err
		}
		return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{current})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_usage_hourly_pending_node ON " + table).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN node_id").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "restore") || db.Migrator().HasColumn(table, "node_id") {
		t.Fatal("active hourly coverage silently defaulted", err)
	}
	// Simulate explicit restoration from the retained source, never startup
	// inference. The interrupted schema must then be resumable.
	if err := db.Exec(statements[3]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(table).Where("traffic_record_id = ?", current.ID).Update("node_id", current.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	run(0, 1)
	if count, err := worker.Drain(t.Context(), 200); err != nil || count != 1 {
		t.Fatal("restored current coverage", count, err)
	}
	assertHourlyTotals(t, db, 2, 14, 6, 8, 24)
	var daily int64
	if err := db.Table("protocol_endpoint_usage_daily").Select("SUM(used_bytes)").Scan(&daily).Error; err != nil || daily != 24 {
		t.Fatal("daily settled deltas changed", daily, err)
	}
	for _, original := range []model.TrafficRecord{fact, current} {
		var stored model.TrafficRecord
		if err := db.First(&stored, original.ID).Error; err != nil || stored.Meta != original.Meta || stored.UsedBytes != original.UsedBytes {
			t.Fatal("schema recovery changed immutable source", err)
		}
	}
}

func assertHourlyBatchDefinition(t *testing.T, db *gorm.DB) {
	t.Helper()
	var columns []struct {
		Name, Type, Nullable string
		Default              *string
		Precision            *int
	}
	if err := db.Raw("SELECT column_name AS name,column_type AS type,is_nullable AS nullable,column_default AS `default`,datetime_precision AS `precision` FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='metering_usage_projection_pending'").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{"hourly_pending": "tinyint(1)", "user_id": "bigint unsigned", "subscription_id": "bigint unsigned", "node_id": "bigint unsigned", "record_at": "datetime(3)"}
	for _, column := range columns {
		typeName, ok := wanted[column.Name]
		if !ok {
			continue
		}
		if column.Type != typeName || (column.Name == "record_at" && (column.Nullable != "YES" || column.Default != nil || column.Precision == nil || *column.Precision != 3)) ||
			(column.Name != "record_at" && (column.Nullable != "NO" || column.Default == nil || *column.Default != "0")) {
			t.Fatalf("cutover column contract changed: %+v", column)
		}
		delete(wanted, column.Name)
	}
	if len(wanted) != 0 {
		t.Fatal("cutover columns missing", fmt.Sprint(wanted))
	}
	assertReadIndexColumns(t, db, "metering_usage_projection_pending", map[string][]string{
		"idx_usage_hourly_pending_time":         {"hourly_pending", "record_at"},
		"idx_usage_hourly_pending_user":         {"hourly_pending", "user_id", "record_at"},
		"idx_usage_hourly_pending_subscription": {"hourly_pending", "subscription_id", "record_at"},
		"idx_usage_hourly_pending_node":         {"hourly_pending", "node_id", "record_at"},
		"idx_usage_hourly_pending_endpoint":     {"hourly_pending", "protocol_endpoint_id", "record_at"},
	})
}
