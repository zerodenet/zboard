package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteMaintenanceSourceFailsClosedAndRecovers(t *testing.T) {
	assertMaintenanceSourceFailsClosedAndRecovers(t, false)
}
func TestMySQLMaintenanceSourceFailsClosedAndRecovers(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("ZBOARD_TEST_MYSQL_DSN is not configured; real MySQL not verified")
	}
	assertMaintenanceSourceFailsClosedAndRecovers(t, true)
}

func maintenanceSourceDatabase(t *testing.T, useMySQL bool) *gorm.DB {
	t.Helper()
	driver, source := datastore.DriverSQLite, filepath.Join(t.TempDir(), "maintenance-source.db")
	if useMySQL {
		cfg, err := mysqlDriver.ParseDSN(os.Getenv("ZBOARD_TEST_MYSQL_DSN"))
		if err != nil {
			t.Fatal("invalid isolated MySQL test configuration")
		}
		cfg.DBName = ""
		admin, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal("cannot open isolated MySQL test server")
		}
		t.Cleanup(func() { _ = admin.Close() })
		name := "zboard_maintenance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`") })
		cfg.DBName, cfg.ParseTime, cfg.Loc = name, true, time.UTC
		driver, source = datastore.DriverMySQL, cfg.FormatDSN()
	}
	db, err := datastore.OpenWithDriver(driver, source)
	if err != nil {
		t.Fatal("open isolated maintenance database failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertMaintenanceSourceFailsClosedAndRecovers(t *testing.T, useMySQL bool) {
	db := maintenanceSourceDatabase(t, useMySQL)
	app := New(db, "maintenance-source-test")
	t.Cleanup(app.Close)
	// Use the same existing defaults boundary as production before StartWork.
	if err := app.SystemConfigDefaults.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	app.StartWork()
	var originalEnabled, originalTask model.SystemConfig
	if err := db.Where("config_key = ?", "maintenance_enabled").First(&originalEnabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("config_key = ?", "maintenance_task_id").First(&originalTask).Error; err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(db)
	active, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "foreign-maintenance-source-running", Handler: "foreign-maintenance-source", Resource: "foreign:maintenance-source", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(t.Context(), "other-process", []string{"foreign-maintenance-source"}, time.Hour)
	if err != nil || lease.Run.ID != active.ID {
		t.Fatal("foreign lease not established", err)
	}
	if _, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "foreign-maintenance-source-queued", Handler: "foreign-maintenance-source", Resource: "foreign:maintenance-source", Payload: `{}`}); err != nil {
		t.Fatal(err)
	}

	snapshot := func(t *testing.T) [32]byte {
		t.Helper()
		var configs []model.SystemConfig
		var tasks []model.Task
		var runs []jobstore.Record
		var attempts []jobstore.Attempt
		var schedules []jobstore.Schedule
		var audits []model.AuditLog
		for _, entry := range []struct {
			target any
			order  string
		}{{&configs, "id"}, {&tasks, "id"}, {&runs, "id"}, {&attempts, "token"}, {&schedules, "id"}, {&audits, "id"}} {
			if err := db.Order(entry.order).Find(entry.target).Error; err != nil {
				t.Fatal(err)
			}
		}
		raw, err := json.Marshal([]any{configs, tasks, runs, attempts, schedules, audits})
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(raw)
	}
	restore := func(t *testing.T) {
		t.Helper()
		for _, row := range []model.SystemConfig{originalEnabled, originalTask} {
			var current model.SystemConfig
			result := db.Where("config_key = ?", row.ConfigKey).First(&current)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			} else if result.Error != nil {
				t.Fatal(result.Error)
			} else {
				if err := db.Model(&current).Updates(map[string]any{"value": row.Value, "revision": row.Revision}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		app.InvalidateMaintenance()
	}
	set := func(t *testing.T, key string, value any) {
		t.Helper()
		if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	remove := func(t *testing.T, key string) {
		t.Helper()
		if err := db.Where("config_key = ?", key).Delete(&model.SystemConfig{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	queryFailure := errors.New("injected maintenance configuration read failure")
	check := func(t *testing.T, wantError, wantPaused, injectQueryFailure bool, forbidden string) {
		t.Helper()
		before := snapshot(t)
		writes := 0
		rejectWrite := func(tx *gorm.DB) { writes++; tx.AddError(errors.New("maintenance read attempted a write")) }
		const writeHook = "test:maintenance-source-read-only"
		if err := db.Callback().Create().Before("gorm:create").Register(writeHook, rejectWrite); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Create().Remove(writeHook)
		if err := db.Callback().Update().Before("gorm:update").Register(writeHook, rejectWrite); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Update().Remove(writeHook)
		if err := db.Callback().Delete().Before("gorm:delete").Register(writeHook, rejectWrite); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Delete().Remove(writeHook)
		if err := db.Callback().Raw().Before("gorm:raw").Register(writeHook, func(tx *gorm.DB) {
			sql := strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String()))
			if !strings.HasPrefix(sql, "SELECT ") && !strings.HasPrefix(sql, "PRAGMA ") {
				rejectWrite(tx)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer db.Callback().Raw().Remove(writeHook)
		const queryHook = "test:maintenance-source-read-error"
		if injectQueryFailure {
			if err := db.Callback().Query().Before("gorm:query").Register(queryHook, func(tx *gorm.DB) {
				if tx.Statement.Table == "system_configs" {
					tx.AddError(queryFailure)
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
		queryHookRemoved := !injectQueryFailure
		defer func() {
			if !queryHookRemoved {
				db.Callback().Query().Remove(queryHook)
			}
		}()
		repository := platformstore.Maintenance{DB: db}
		data, readErr := repository.Read(t.Context())
		app.InvalidateMaintenance()
		state, stateErr := app.MaintenanceState(t.Context(), true)
		if injectQueryFailure {
			db.Callback().Query().Remove(queryHook)
			queryHookRemoved = true
		}
		for _, err := range []error{readErr, stateErr} {
			if wantError {
				expected := platform.ErrMaintenanceIncomplete
				if injectQueryFailure {
					expected = queryFailure
				}
				if !errors.Is(err, expected) {
					t.Fatal("maintenance source did not return the expected classified error", err)
				}
				if forbidden != "" && strings.Contains(err.Error(), forbidden) {
					t.Fatal("maintenance source error exposed its invalid stored value")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if !wantError && (data.State().Enabled != wantPaused || state.Enabled != wantPaused) {
			t.Fatal("valid maintenance facts did not preserve their gate semantics")
		}
		// The error cached above must keep ordinary work paused after the injected
		// SQL fault is gone; no local registration or global cancellation is used.
		if app.WorkPaused() != wantPaused || app.WorkPaused() != wantPaused {
			t.Fatal("application work gate did not preserve unknown/known source state")
		}
		if writes != 0 || snapshot(t) != before {
			t.Fatal("read/work gate mutated configuration, revisions, tasks, lease, queued intent, attempt, schedule or audit")
		}
	}
	for _, tc := range []struct {
		name, key string
		value     any
		missing   bool
		forbidden string
	}{
		{name: "missing_enabled", key: "maintenance_enabled", missing: true},
		{name: "invalid_enabled", key: "maintenance_enabled", value: "private-invalid-boolean", forbidden: "private-invalid-boolean"},
		{name: "empty_enabled", key: "maintenance_enabled", value: ""},
		{name: "null_enabled", key: "maintenance_enabled", value: nil},
		{name: "invalid_task_id", key: "maintenance_task_id", value: "private-invalid-task", forbidden: "private-invalid-task"},
		{name: "negative_task_id", key: "maintenance_task_id", value: "-1"},
		{name: "overflow_task_id", key: "maintenance_task_id", value: "18446744073709551616"},
		{name: "empty_task_id", key: "maintenance_task_id", value: ""},
		{name: "null_task_id", key: "maintenance_task_id", value: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore(t)
			if tc.missing {
				remove(t, tc.key)
			} else {
				set(t, tc.key, tc.value)
			}
			check(t, true, true, false, tc.forbidden)
			restore(t)
			check(t, false, false, false, "")
		})
	}
	for _, tc := range []struct {
		value   string
		enabled bool
	}{{"false", false}, {"FALSE", false}, {"0", false}, {"true", true}, {"TRUE", true}, {"1", true}} {
		t.Run("valid_boolean_"+tc.value, func(t *testing.T) {
			restore(t)
			set(t, "maintenance_enabled", tc.value)
			check(t, false, tc.enabled, false, "")
		})
	}
	t.Run("legacy_task_id_missing", func(t *testing.T) {
		restore(t)
		remove(t, "maintenance_task_id")
		check(t, false, false, false, "")
	})
	t.Run("legacy_stale_task_id", func(t *testing.T) {
		restore(t)
		set(t, "maintenance_task_id", "999999")
		check(t, false, false, false, "")
	})
	t.Run("legacy_zero_padded_task_id_and_zero_revision", func(t *testing.T) {
		restore(t)
		set(t, "maintenance_task_id", "0000")
		if err := db.Model(&model.SystemConfig{}).Where("config_key IN ?", []string{"maintenance_enabled", "maintenance_task_id"}).Update("revision", 0).Error; err != nil {
			t.Fatal(err)
		}
		check(t, false, false, false, "")
	})
	t.Run("missing_task_id_active_task_fallback", func(t *testing.T) {
		restore(t)
		remove(t, "maintenance_task_id")
		task := model.Task{Type: "database_migration", Scope: "{}", Content: `"protected-test-fixture"`, Status: 0, Total: 1, MaxAttempts: 1}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		check(t, false, true, false, "")
		if err := db.Model(&task).Update("status", 2).Error; err != nil {
			t.Fatal(err)
		}
		check(t, false, false, false, "")
	})
	t.Run("missing_task_id_reserved_run_fallback", func(t *testing.T) {
		restore(t)
		remove(t, "maintenance_task_id")
		reserved, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "maintenance-source-reservation", Handler: "database_migration", Resource: jobs.MaintenanceResource, Payload: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		check(t, false, true, false, "")
		// Fixture-only resolution of this exact reservation, never CancelHandler.
		if err := db.Model(&jobstore.Record{}).Where("id = ?", reserved.ID).Update("state", jobs.Canceled).Error; err != nil {
			t.Fatal(err)
		}
		check(t, false, false, false, "")
	})
	t.Run("sql_read_failure_and_recovery", func(t *testing.T) {
		restore(t)
		check(t, true, true, true, "")
		app.InvalidateMaintenance()
		check(t, false, false, false, "")
	})
	t.Run("actual_sql_source_unavailable_and_recovery", func(t *testing.T) {
		restore(t)
		before := snapshot(t)
		const unavailableTable = "maintenance_source_unavailable"
		if err := db.Migrator().RenameTable("system_configs", unavailableTable); err != nil {
			t.Fatal(err)
		}
		restored := false
		defer func() {
			if !restored {
				if err := db.Migrator().RenameTable(unavailableTable, "system_configs"); err != nil {
					t.Error("restore configuration source table", err)
				}
			}
		}()
		if _, err := (platformstore.Maintenance{DB: db}).Read(t.Context()); err == nil {
			t.Fatal("missing SQL source returned a successful repository read")
		}
		app.InvalidateMaintenance()
		if _, err := app.MaintenanceState(t.Context(), true); err == nil || !app.WorkPaused() {
			t.Fatal("missing SQL source did not pause work")
		}
		if err := db.Migrator().RenameTable(unavailableTable, "system_configs"); err != nil {
			t.Fatal(err)
		}
		restored = true
		if snapshot(t) != before {
			t.Fatal("source failure changed configuration or canceled durable foreign work")
		}
		app.InvalidateMaintenance()
		check(t, false, false, false, "")
	})
	restore(t)
	if err := store.Finish(context.Background(), lease, jobs.Succeeded); err != nil {
		t.Fatal("read errors invalidated another process's completion fence", err)
	}
}
