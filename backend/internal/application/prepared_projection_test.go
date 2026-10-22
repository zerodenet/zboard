package application

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestSQLitePreparedProjectionDatabaseReadOnly(t *testing.T) {
	assertPreparedProjectionDatabase(t, datastore.DriverSQLite)
}
func TestMySQLPreparedProjectionDatabaseReadOnly(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("isolated MySQL opt-in is not configured")
	}
	assertPreparedProjectionDatabase(t, datastore.DriverMySQL)
}

// Snapshot every current application table, including financial facts and
// receipt/job/queue/config values, plus physical DDL. Only setup/fault injection
// may change these; validation must not alter even an invalid database.
func preparedProjectionSnapshot(t *testing.T, db *gorm.DB) [32]byte {
	t.Helper()
	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	material := map[string]any{}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var rows []map[string]any
		if err = db.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		// Sort row encodings, independent of a physical table scan's row order.
		encoded := make([]string, 0, len(rows))
		for _, row := range rows {
			b, e := json.Marshal(row)
			if e != nil {
				t.Fatal(e)
			}
			encoded = append(encoded, string(b))
		}
		sort.Strings(encoded)
		material[table+":rows"] = encoded
		if datastore.IsSQLite(db) {
			var ddl []struct{ Type, Name, SQL string }
			if err = db.Raw("SELECT type,name,COALESCE(sql,'') AS sql FROM sqlite_master WHERE tbl_name=? ORDER BY type,name", table).Scan(&ddl).Error; err != nil {
				t.Fatal(err)
			}
			material[table+":ddl"] = ddl
		} else {
			var name, ddl string
			if err = db.Raw("SHOW CREATE TABLE `"+strings.ReplaceAll(table, "`", "``")+"`").Row().Scan(&name, &ddl); err != nil {
				t.Fatal(err)
			}
			material[table+":ddl"] = ddl
		}
	}
	b, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}
func assertPreparedProjectionDatabase(t *testing.T, driver string) {
	db := schemaFixture(t, driver)
	mysql := driver == datastore.DriverMySQL
	check := func(t *testing.T, want error) error {
		t.Helper()
		before := preparedProjectionSnapshot(t, db)
		cleanup := guardPreparedProjectionReadOnly(t, db)
		err := ValidatePreparedProjectionDatabase(t.Context(), db)
		cleanup()
		if want == nil && err != nil {
			t.Fatal("prepared database rejected", err)
		}
		if want != nil && !errors.Is(err, want) {
			t.Fatal("invalid database not refused with typed error", err)
		}
		if after := preparedProjectionSnapshot(t, db); after != before {
			t.Fatal("validator changed business rows or DDL")
		}
		return err
	}
	t.Run("fresh_unprepared", func(t *testing.T) {
		if err := ValidatePreparedProjectionDatabase(t.Context(), db); !errors.Is(err, datastore.ErrProjectionNotPrepared) {
			t.Fatal(err)
		}
		tables, err := db.Migrator().GetTables()
		if err != nil || len(tables) != 0 {
			t.Fatal("validator bootstrapped fresh database", err)
		}
	})
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := (platformstoreDefaults(db)).Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Run("prepared_repeated", func(t *testing.T) { check(t, nil); check(t, nil) })
	t.Run("receipt_required", func(t *testing.T) {
		var receipt map[string]any
		if err := db.Table("schema_migrations").Where("version=?", "0054_hourly_usage_projection.up.sql").Take(&receipt).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", "0054_hourly_usage_projection.up.sql").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Table("schema_migrations").Create(receipt).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("retained_development_history", func(t *testing.T) {
		old := "0032_subscription_policy_group_targets.up.sql"
		if err := db.Table("schema_migrations").Create(map[string]any{"version": old, "applied_at": time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
		check(t, nil)
		if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", old).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unrelated_commerce_receipt_not_dependency", func(t *testing.T) {
		var receipt map[string]any
		if err := db.Table("schema_migrations").Where("version=?", "0055_discount_purchase_snapshot.up.sql").Take(&receipt).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", "0055_discount_purchase_snapshot.up.sql").Error; err != nil {
			t.Fatal(err)
		}
		check(t, nil)
		if err := db.Table("schema_migrations").Create(receipt).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("required_index_shape", func(t *testing.T) {
		drop := "DROP INDEX job_ready"
		if mysql {
			drop += " ON job_runs"
		}
		if err := db.Exec(drop).Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec("CREATE INDEX job_ready ON job_runs(not_before,state)").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec(drop).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE INDEX job_ready ON job_runs(state,not_before)").Error; err != nil {
			t.Fatal(err)
		}
	})
	for _, fault := range []string{"missing_column", "wrong_type", "missing_primary", "nullable_column"} {
		t.Run(fault, func(t *testing.T) {
			if err := db.Exec("ALTER TABLE job_dispatch_lanes RENAME TO saved_projection_lanes").Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Exec("DROP TABLE IF EXISTS job_dispatch_lanes").Error; err != nil {
					t.Error(err)
				}
				if err := db.Exec("ALTER TABLE saved_projection_lanes RENAME TO job_dispatch_lanes").Error; err != nil {
					t.Error(err)
				}
			})
			definition := "lane TEXT PRIMARY KEY,virtual_finish INTEGER NOT NULL"
			if mysql {
				definition = "lane varchar(200) PRIMARY KEY,virtual_finish bigint unsigned NOT NULL"
			}
			switch fault {
			case "missing_column":
				definition = "lane TEXT PRIMARY KEY"
				if mysql {
					definition = "lane varchar(200) PRIMARY KEY"
				}
			case "wrong_type":
				definition = "lane TEXT PRIMARY KEY,virtual_finish TEXT NOT NULL"
				if mysql {
					definition = "lane varchar(200) PRIMARY KEY,virtual_finish varchar(200) NOT NULL"
				}
			case "nullable_column":
				definition = "lane TEXT PRIMARY KEY,virtual_finish INTEGER"
				if mysql {
					definition = "lane varchar(200) PRIMARY KEY,virtual_finish bigint unsigned NULL"
				}
			case "missing_primary":
				definition = "lane TEXT NOT NULL,virtual_finish INTEGER NOT NULL"
				if mysql {
					definition = "lane varchar(200) NOT NULL,virtual_finish bigint unsigned NOT NULL"
				}
			}
			suffix := ""
			if mysql {
				suffix = " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin"
			}
			if err := db.Exec("CREATE TABLE job_dispatch_lanes(" + definition + ")" + suffix).Error; err != nil {
				t.Fatal(err)
			}
			err := check(t, datastore.ErrProjectionNotPrepared)
			detail := "job column type differs: job_dispatch_lanes.virtual_finish"
			switch fault {
			case "missing_column":
				detail = "job column missing: job_dispatch_lanes.virtual_finish"
			case "missing_primary":
				detail = "job primary key differs: job_dispatch_lanes"
			case "nullable_column":
				detail = "job column nullable: job_dispatch_lanes.virtual_finish"
			}
			if !strings.Contains(err.Error(), detail) {
				t.Fatal("fault did not reach its intended physical contract", err)
			}

		})
	}
	if mysql {
		t.Run("unsigned_job_counter_required", func(t *testing.T) {
			if err := db.Exec("ALTER TABLE job_dispatch_lanes MODIFY virtual_finish bigint NOT NULL").Error; err != nil {
				t.Fatal(err)
			}
			err := check(t, datastore.ErrProjectionNotPrepared)
			if !strings.Contains(err.Error(), "job column type differs: job_dispatch_lanes.virtual_finish") {
				t.Fatal("signedness fault reached another contract", err)
			}
			if err := db.Exec("ALTER TABLE job_dispatch_lanes MODIFY virtual_finish bigint unsigned NOT NULL").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("observation_partial", func(t *testing.T) {
		if err := db.Exec("ALTER TABLE node_observation_parts RENAME TO saved_projection_parts").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec("ALTER TABLE saved_projection_parts RENAME TO node_observation_parts").Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("global_budget_required", func(t *testing.T) {
		var budget map[string]any
		if err := db.Table("job_execution_budget").Where("id=1").Take(&budget).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM job_execution_budget WHERE id=1").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Table("job_execution_budget").Create(budget).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("legacy_hourly_trigger", func(t *testing.T) {
		statement := "CREATE TRIGGER traffic_hourly_insert AFTER INSERT ON traffic_records BEGIN SELECT 1; END"
		if mysql {
			statement = "CREATE TRIGGER traffic_hourly_insert BEFORE INSERT ON traffic_records FOR EACH ROW SET @zboard_projection_probe=1"
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec("DROP TRIGGER traffic_hourly_insert").Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("maintenance_source", func(t *testing.T) {
		var enabled model.SystemConfig
		if err := db.Where("config_key=?", "maintenance_enabled").First(&enabled).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM system_configs WHERE config_key=?", "maintenance_enabled").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Create(&enabled).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&enabled).Update("value", "not-a-valid-switch-sensitive").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Model(&enabled).Update("value", "false").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM system_configs WHERE config_key=?", "maintenance_task_id").Error; err != nil {
			t.Fatal(err)
		}
		check(t, nil)
		if err := db.Create(&model.SystemConfig{ConfigKey: "maintenance_task_id", Value: "invalid-sensitive-task", ValueType: "int"}).Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec("DELETE FROM system_configs WHERE config_key=?", "maintenance_task_id").Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("metadata_query_error", func(t *testing.T) {
		sentinel := errors.New("injected metadata error")
		const cb = "test:prepared_projection_metadata"
		if err := db.Callback().Row().Before("gorm:row").Register(cb, func(tx *gorm.DB) {
			sql := tx.Statement.SQL.String()
			if strings.Contains(sql, "INFORMATION_SCHEMA") || strings.Contains(sql, "sqlite_master") {
				tx.AddError(sentinel)
			}
		}); err != nil {
			t.Fatal(err)
		}
		err := ValidatePreparedProjectionDatabase(t.Context(), db)
		if e := db.Callback().Row().Remove(cb); e != nil {
			t.Fatal(e)
		}
		if !errors.Is(err, sentinel) || !errors.Is(err, datastore.ErrProjectionNotPrepared) {
			t.Fatal("query cause lost", err)
		}
	})
	if mysql {
		t.Run("read_only_transaction", func(t *testing.T) {
			before := preparedProjectionSnapshot(t, db)
			err := db.Transaction(func(tx *gorm.DB) error { return ValidatePreparedProjectionDatabase(t.Context(), tx) }, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal("read-only transaction rejected prepared dependencies", err)
			}
			if preparedProjectionSnapshot(t, db) != before {
				t.Fatal("read-only validation mutated database")
			}
		})
		t.Run("check_connection_read_only_transaction", func(t *testing.T) {
			dialect, ok := db.Dialector.(*gormmysql.Dialector)
			if !ok || dialect.DSN == "" {
				t.Fatal("isolated MySQL source configuration missing")
			}
			before := preparedProjectionSnapshot(t, db)
			read, err := datastore.OpenProjectionCheckDatabase(t.Context(), datastore.DriverMySQL, dialect.DSN, datastore.DefaultPoolConfig())
			if err != nil {
				t.Fatal("check-mode connection failed", err)
			}
			pool, err := read.DB()
			if err != nil {
				t.Fatal("check-mode pool failed")
			}
			defer pool.Close()
			if err := read.Transaction(func(tx *gorm.DB) error { return ValidatePreparedProjectionDatabase(t.Context(), tx) }, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}); err != nil {
				t.Fatal("actual check-mode read-only connection rejected prepared source", err)
			}
			if preparedProjectionSnapshot(t, db) != before {
				t.Fatal("check-mode connection changed schema or business rows")
			}
		})
	} else {
		t.Run("physical_read_only_pool", func(t *testing.T) {
			read, close, err := datastore.OpenReadView(db)
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			if read == db {
				t.Fatal("fixture did not create separate physical read-only pool")
			}
			before := preparedProjectionSnapshot(t, db)
			if err := ValidatePreparedProjectionDatabase(t.Context(), read); err != nil {
				t.Fatal(err)
			}
			if preparedProjectionSnapshot(t, db) != before {
				t.Fatal("read-only validation mutated source")
			}
		})
	}
	check(t, nil)
}
func platformstoreDefaults(db *gorm.DB) platform.SystemConfigDefaults {
	return platform.SystemConfigDefaults{Repository: platformstore.SystemConfigDefaults{DB: db}}
}

// This catches attempted no-op writes too; snapshot equality alone cannot.
func guardPreparedProjectionReadOnly(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	const name = "test:prepared_projection_read_only"
	reject := func(tx *gorm.DB) { tx.AddError(errors.New("validator attempted a write")) }
	if err := db.Callback().Create().Before("gorm:create").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
		query := strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String()))
		if !strings.HasPrefix(query, "SELECT ") && !strings.HasPrefix(query, "SHOW ") {
			reject(tx)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := db.Callback().Create().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Update().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Delete().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Raw().Remove(name); err != nil {
			t.Fatal(err)
		}
	}
}
