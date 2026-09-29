package datastore_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const hourlyVersion = "0024_traffic_usage_hourly.up.sql"
const hourlyAppVersion = "0025_traffic_hourly_application.up.sql"

// Never touch the supplied schema: root is used only to provision a disposable
// schema and an account with ALL on that schema, without any global privilege.
func hourlyMySQLFixture(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN to a disposable MySQL server with binlog ON and function trust OFF")
	}
	config, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName, config.ParseTime, config.Loc = "", true, time.UTC
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
	admin, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, user := "zboard_test_"+suffix, "zh_"+suffix[:24]
	for _, statement := range []string{
		"CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4",
		"CREATE USER '" + user + "'@'%' IDENTIFIED BY 'hourly-test-password'",
		"GRANT ALL ON `" + schema + "`.* TO '" + user + "'@'%'",
	} {
		if _, err := admin.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE `" + schema + "`")
		_, _ = admin.Exec("DROP USER '" + user + "'@'%'")
	})
	config.DBName = schema
	root, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	config.User, config.Passwd = user, "hourly-test-password"
	db, err := datastore.Open(config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	db.Logger = logger.Discard
	t.Cleanup(func() { _ = pool.Close() })
	var binlog, trust int
	if err := root.QueryRow("SELECT @@log_bin,@@log_bin_trust_function_creators").Scan(&binlog, &trust); err != nil || binlog != 1 || trust != 0 {
		t.Fatalf("requires binlog=1 trust=0, got %d/%d: %v", binlog, trust, err)
	}
	return db, root
}

func TestMySQLHourlyMigrationRecoversUnprivilegedBinlogInstall(t *testing.T) {
	payload, err := migrations.Files.ReadFile(hourlyVersion)
	if err != nil {
		t.Fatal(err)
	}
	original := strings.Split(strings.TrimSpace(string(payload)), ";")
	for _, completed := range []int{0, 1, 3, 6, 7, 10} {
		t.Run(fmt.Sprintf("committed_statements_%d", completed), func(t *testing.T) {
			db, root := hourlyMySQLFixture(t)
			if err := datastore.RunMigrations(db); err != nil {
				t.Fatal(err)
			}
			for _, parent := range []any{
				&model.User{ID: 2, Email: "hourly@example.test", Password: "unused", Status: "active"},
				&model.Node{ID: 4, Name: "hourly-node", Address: "localhost", Config: "{}"},
				&model.ProtocolEndpoint{ID: 5, NodeID: 4, Name: "hourly-endpoint", Protocol: "vless", ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000},
			} {
				if err := db.Create(parent).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Exec("DELETE FROM schema_migrations WHERE version IN (?,?)", hourlyVersion, hourlyAppVersion).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DROP TABLE traffic_usage_hourly").Error; err != nil {
				t.Fatal(err)
			}
			// Nullable legacy dimensions and multiple buckets must backfill exactly.
			if err := db.Exec(`INSERT INTO traffic_records (user_id,subscription_id,node_id,report_id,nonce,protocol_endpoint_id,protocol_multiplier_milli,record_at,raw_bytes,upload_bytes,download_bytes,used_bytes) VALUES
    (NULL,NULL,NULL,'old-a','old-a',NULL,1000,'2026-09-28 01:15:00',20,7,13,10),
    (2,3,4,'old-b','old-b',5,1000,'2026-09-28 02:15:00',50,20,30,30),
    (2,3,4,'old-c','old-c',5,1000,'2026-09-28 02:45:00',70,30,40,40)`).Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < completed; i++ {
				if _, err := root.Exec(original[i]); err != nil {
					t.Fatalf("simulate old statement %d: %v", i+1, err)
				}
			}
			if completed == 6 {
				err := db.Exec(original[6]).Error
				var mysqlErr *mysqlconfig.MySQLError
				if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1419 {
					t.Fatalf("original trigger must reproduce 1419: %v", err)
				}
				err = db.Exec(original[0]).Error
				if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1050 {
					t.Fatalf("original retry must reproduce 1050: %v", err)
				}
			}
			if completed == 10 {
				if err := db.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)", hourlyVersion, time.Now().UTC()).Error; err != nil {
					t.Fatal(err)
				}
			}
			// Poison an already-backfilled projection: rebuild replaces, never adds.
			if completed >= 6 {
				if err := db.Exec("UPDATE traffic_usage_hourly SET used_bytes = 999999").Error; err != nil {
					t.Fatal(err)
				}
			}
			if completed == 6 {
				if err := db.Exec("ALTER TABLE schema_migrations ADD CONSTRAINT reject_hourly_completion CHECK (version <> '0025_traffic_hourly_application.up.sql')").Error; err != nil {
					t.Fatal(err)
				}
				if err := datastore.RunMigrations(db); err == nil {
					t.Fatal("accepted failed version recording")
				}
				assertHourlyTotals(t, db, 3, 140, 57, 83, 1999998)
				var marked int64
				if err := db.Table("schema_migrations").Where("version IN (?,?)", hourlyVersion, hourlyAppVersion).Count(&marked).Error; err != nil || marked != 0 {
					t.Fatalf("partial version committed: %d %v", marked, err)
				}
				if err := db.Exec("ALTER TABLE schema_migrations DROP CHECK reject_hourly_completion").Error; err != nil {
					t.Fatal(err)
				}
			}
			for retry := 0; retry < 2; retry++ {
				if err := datastore.RunMigrations(db); err != nil {
					t.Fatalf("fixed startup %d: %v", retry, err)
				}
				assertHourlyTotals(t, db, 3, 140, 57, 83, 80)
			}
			var count int64
			if err := db.Raw("SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE() AND TRIGGER_NAME LIKE 'traffic_hourly_%'").Scan(&count).Error; err != nil || count != 0 {
				t.Fatalf("legacy triggers=%d: %v", count, err)
			}
			if err := db.Table("schema_migrations").Where("version IN (?,?)", hourlyVersion, hourlyAppVersion).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("versions=%d: %v", count, err)
			}
			if err := db.Table("traffic_records").Count(&count).Error; err != nil || count != 3 {
				t.Fatalf("ledger was changed: %d %v", count, err)
			}
			verifyHourlyAccounting(t, db)
		})
	}
}

func assertHourlyTotals(t *testing.T, db *gorm.DB, count, raw, up, down, used int64) {
	t.Helper()
	var got struct{ Count, Raw, Up, Down, Used int64 }
	if err := db.Table("traffic_usage_hourly").Select("COALESCE(SUM(record_count),0) AS count,COALESCE(SUM(raw_bytes),0) AS raw,COALESCE(SUM(upload_bytes),0) AS up,COALESCE(SUM(download_bytes),0) AS down,COALESCE(SUM(used_bytes),0) AS used").Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Count != count || got.Raw != raw || got.Up != up || got.Down != down || got.Used != used {
		t.Fatalf("hourly=%+v, want %d/%d/%d/%d/%d", got, count, raw, up, down, used)
	}
}

func TestMySQLHourlyUpgradeRejectsHiddenLegacyTrigger(t *testing.T) {
	db, root := hourlyMySQLFixture(t)
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	payload, err := migrations.Files.ReadFile(hourlyVersion)
	if err != nil {
		t.Fatal(err)
	}
	original := strings.Split(strings.TrimSpace(string(payload)), ";")
	if _, err := root.Exec(original[6]); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", hourlyAppVersion).Error; err != nil {
		t.Fatal(err)
	}
	var identity struct{ Schema, Account string }
	if err := db.Raw("SELECT DATABASE() AS `schema`, SUBSTRING_INDEX(CURRENT_USER(), '@', 1) AS account").Scan(&identity).Error; err != nil {
		t.Fatal(err)
	}
	grantTarget := " ON `" + identity.Schema + "`.* "
	account := "'" + identity.Account + "'@'%'"
	if _, err := root.Exec("REVOKE TRIGGER" + grantTarget + "FROM " + account); err != nil {
		t.Fatal(err)
	}
	// Database-level privilege changes require a new session to take effect.
	pool, _ := db.DB()
	pool.SetMaxIdleConns(0)
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("upgrade left an invisible trigger as a second projection writer")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", hourlyAppVersion).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unsafe upgrade recorded: %d %v", count, err)
	}
	if _, err := root.Exec("GRANT TRIGGER" + grantTarget + "TO " + account); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
}

func verifyHourlyAccounting(t *testing.T, db *gorm.DB) {
	t.Helper()
	records := []model.TrafficRecord{
		{UserID: 2, SubscriptionID: 3, NodeID: 4, ProtocolEndpointID: 5, ProtocolMultiplierMilli: 1000, ReportID: "new-a", Nonce: "new-a", At: time.Date(2026, 9, 28, 2, 20, 0, 0, time.UTC), RawBytes: 7, UploadBytes: 2, DownloadBytes: 5, UsedBytes: 6},
		{UserID: 2, SubscriptionID: 3, NodeID: 4, ProtocolEndpointID: 5, ProtocolMultiplierMilli: 1000, ReportID: "new-b", Nonce: "new-b", At: time.Date(2026, 9, 28, 3, 20, 0, 0, time.FixedZone("UTC+8", 8*3600)), RawBytes: 9, UploadBytes: 3, DownloadBytes: 6, UsedBytes: 8},
	}
	write := func(tx *gorm.DB) error {
		if err := tx.Create(&records).Error; err != nil {
			return err
		}
		return meteringstore.RecordUsageProjections(tx, records)
	}
	if err := db.Transaction(write); err != nil {
		t.Fatal(err)
	}
	assertHourlyTotals(t, db, 5, 156, 62, 94, 94)
	// A duplicate fails before the projection; the transaction cannot add twice.
	records[0].ID, records[1].ID = 0, 0
	if err := db.Transaction(write); err == nil {
		t.Fatal("duplicate accepted")
	}
	assertHourlyTotals(t, db, 5, 156, 62, 94, 94)
	records[0].ReportID, records[0].Nonce = "rollback-a", "rollback-a"
	records[1].ReportID, records[1].Nonce = "rollback-b", "rollback-b"
	records[0].ID, records[1].ID = 0, 0
	abort := errors.New("abort")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := write(tx); err != nil {
			return err
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	assertHourlyTotals(t, db, 5, 156, 62, 94, 94)
	// Database transfers rebuild derived state inside the destination transaction.
	if err := db.Transaction(func(tx *gorm.DB) error { return datastore.RebuildMySQLTrafficHourly(tx) }); err != nil {
		t.Fatal(err)
	}
	assertHourlyTotals(t, db, 5, 156, 62, 94, 94)
	from := time.Date(2026, 9, 27, 19, 10, 0, 0, time.UTC)
	to := time.Date(2026, 9, 28, 2, 30, 0, 0, time.UTC)
	var ledger, projected int64
	if err := db.Table("traffic_records").Where("subscription_id = ? AND record_at >= ? AND record_at < ?", 3, from, to).Select("COALESCE(SUM(used_bytes),0)").Scan(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if err := meteringstore.TrafficAggregateSource(db, from, to, meteringstore.TrafficScope{SubscriptionID: 3}).Select("COALESCE(SUM(used_bytes),0)").Scan(&projected).Error; err != nil || projected != ledger {
		t.Fatalf("partial/scoped aggregate=%d ledger=%d: %v", projected, ledger, err)
	}
}
