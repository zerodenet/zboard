package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProjectionDatabaseCheckModeExclusion(t *testing.T) {
	for _, tc := range []struct{ migrate, rollback, check, wantError bool }{
		{false, false, false, false}, {true, false, false, false}, {false, true, false, false}, {false, false, true, false},
		{true, true, false, true}, {true, false, true, true}, {false, true, true, true}, {true, true, true, true},
	} {
		err := validateDatabaseModes(tc.migrate, tc.rollback, tc.check)
		if (err != nil) != tc.wantError {
			t.Fatal("mode exclusion mismatch")
		}
	}
}

// The child calls actual main, with the real flag/config/open/ping/validation
// path. No HTTP/client mocks or alternate preparation callback are injected.
func TestProjectionDatabaseCheckEntrypoint(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_PROJECTION_CHECK_CHILD") == "1" {
		main()
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "prepared.db")
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := (platformstore.SystemConfigDefaults{DB: db}).ReconcileSystemConfigDefaults(t.Context(), platform.DefaultSystemConfigs()); err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "check-only-node", Address: "192.0.2.250", Config: "{}", NodeCredential: "test-legacy-plaintext-must-not-migrate"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	config := cfgpkg.Config{Environment: cfgpkg.EnvironmentTest, DatabaseDriver: datastore.DriverSQLite, DataSource: path, JwtSecret: "0123456789abcdef0123456789abcdef", CredentialEncryptionKey: strings.Repeat("0123456789abcdef", 4), BootstrapAdminEmail: "check-only-admin@example.test", BootstrapAdminPassword: "only-test-bootstrap-password", ZeroEventSpoolDir: filepath.Join(dir, "must-not-open-spool"), FileStorageDir: filepath.Join(dir, "must-not-open-files")}
	config.Name = "projection-check-test"
	config.Host = "127.0.0.1"
	config.Port = 18443
	configPath := filepath.Join(dir, "check.json")
	writeConfig := func() {
		t.Helper()
		raw, e := json.Marshal(map[string]any{
			"Name": config.Name, "Host": config.Host, "Port": config.Port,
			"Log":         map[string]any{"Level": "error", "Mode": "console"},
			"environment": config.Environment, "database_driver": config.DatabaseDriver, "datasource": config.DataSource,
			"jwt_secret": config.JwtSecret, "credential_encryption_key": config.CredentialEncryptionKey,
			"bootstrap_admin_email": config.BootstrapAdminEmail, "bootstrap_admin_password": config.BootstrapAdminPassword,
			"zero_event_spool_dir": config.ZeroEventSpoolDir, "file_storage_dir": config.FileStorageDir,
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(configPath, raw, 0o600); e != nil {
			t.Fatal(e)
		}
	}
	writeConfig()
	child := func(t *testing.T, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestProjectionDatabaseCheckEntrypoint$", "-f", configPath}, args...)...)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "ZBOARD_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "ZBOARD_TEST_PROJECTION_CHECK_CHILD=1")
		return cmd.CombinedOutput()
	}
	assertUnchanged := func(t *testing.T, before [32]byte) {
		t.Helper()
		if projectionCommandSnapshot(t, db) != before {
			t.Fatal("check entrypoint changed data or schema")
		}
		for _, path := range []string{config.ZeroEventSpoolDir, config.FileStorageDir} {
			if _, e := os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("check entrypoint opened server-local storage")
			}
		}
	}
	t.Run("prepared_without_bootstrap_or_credential_migration", func(t *testing.T) {
		before := projectionCommandSnapshot(t, db)
		out, err := child(t, "-check-projection-database")
		if err != nil {
			t.Fatalf("check entrypoint failed: %v; %s", err, out)
		}
		if !strings.Contains(string(out), "no worker started") {
			t.Fatal("check result missing readonly classification")
		}
		assertUnchanged(t, before)
	})
	t.Run("incompatible_modes_fail_before_configuration_or_database", func(t *testing.T) {
		for _, args := range [][]string{{"-check-projection-database", "-migrate-only"}, {"-check-projection-database", "-prepare-hourly-rollback"}} {
			before := projectionCommandSnapshot(t, db)
			out, err := child(t, args...)
			if err == nil || !strings.Contains(string(out), "mutually exclusive") {
				t.Fatal("actual command accepted incompatible modes")
			}
			assertUnchanged(t, before)
		}
	})
	t.Run("bad_schema_fails_without_repair", func(t *testing.T) {
		if err := db.Exec("ALTER TABLE job_dispatch_lanes RENAME TO saved_check_lanes").Error; err != nil {
			t.Fatal(err)
		}
		before := projectionCommandSnapshot(t, db)
		out, err := child(t, "-check-projection-database")
		if err == nil || !strings.Contains(string(out), "projection database check failed") {
			t.Fatal("bad schema command did not refuse")
		}
		assertUnchanged(t, before)
		if err := db.Exec("ALTER TABLE saved_check_lanes RENAME TO job_dispatch_lanes").Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing_file_is_not_created", func(t *testing.T) {
		config.DataSource = filepath.Join(dir, "must-not-create.db")
		writeConfig()
		out, err := child(t, "-check-projection-database")
		if err == nil || !strings.Contains(string(out), "connection initialization") {
			t.Fatal("missing database check unexpectedly succeeded")
		}
		if _, err := os.Stat(config.DataSource); !os.IsNotExist(err) {
			t.Fatal("read-only check created a database file")
		}
	})
}
func projectionCommandSnapshot(t *testing.T, db *gorm.DB) [32]byte {
	t.Helper()
	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	material := map[string]any{}
	for _, table := range tables {
		var rows []map[string]any
		if err := db.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		encoded := make([]string, 0, len(rows))
		for _, row := range rows {
			b, e := json.Marshal(row)
			if e != nil {
				t.Fatal(e)
			}
			encoded = append(encoded, string(b))
		}
		sort.Strings(encoded)
		material[table] = encoded
	}
	var ddl []struct{ Type, Name, Table, SQL string }
	if err := db.Raw("SELECT type,name,tbl_name AS `table`,COALESCE(sql,'') AS sql FROM sqlite_master ORDER BY type,name").Scan(&ddl).Error; err != nil {
		t.Fatal(err)
	}
	material["physical_schema"] = ddl
	b, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}
