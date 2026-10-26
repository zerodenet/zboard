package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProjectionWorkerModeExcludesPreparationBeforeConfiguration(t *testing.T) {
	for bits := range 16 {
		worker, migrate, rollback, check := bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0
		err := validateProjectionWorkerMode(worker, migrate, rollback, check, 30*time.Second)
		if (err != nil) != (worker && (migrate || rollback || check)) {
			t.Fatal("incompatible execution modes admitted", bits)
		}
	}
	for _, value := range []time.Duration{0, time.Millisecond, 5*time.Minute + time.Second} {
		if err := validateProjectionWorkerMode(true, false, false, false, value); err == nil {
			t.Fatal("invalid drain budget admitted")
		}
	}
}

func TestProjectionWorkerDoesNotCreateMissingSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.db")
	c := projectionWorkerTestConfig(path, dir)
	if err := runProjectionWorker(t.Context(), c, time.Second); err == nil {
		t.Fatal("missing database admitted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("worker created database, WAL or local storage", files, err)
	}
}

// Actual main in another process, real flags/config/database/signals. Keeping
// its configured TCP port occupied proves this mode never starts an HTTP server.
func TestProjectionWorkerEntrypoint(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_PROJECTION_WORKER_CHILD") == "1" {
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
	node := model.Node{Name: "worker-command", Config: "{}", NodeCredential: "test-legacy-plaintext-must-not-migrate"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := projectionWorkerTestConfig(path, dir)
	c.Port = listener.Addr().(*net.TCPAddr).Port
	configPath := filepath.Join(dir, "worker.json")
	raw, err := json.Marshal(map[string]any{"Name": c.Name, "Host": c.Host, "Port": c.Port, "Log": map[string]any{"Level": "error", "Mode": "console"}, "environment": c.Environment, "database_driver": c.DatabaseDriver, "datasource": c.DataSource, "jwt_secret": c.JwtSecret, "credential_encryption_key": c.CredentialEncryptionKey, "bootstrap_admin_email": c.BootstrapAdminEmail, "bootstrap_admin_password": c.BootstrapAdminPassword, "zero_event_spool_dir": c.ZeroEventSpoolDir, "file_storage_dir": c.FileStorageDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProjectionWorkerEntrypoint$", "-f", configPath, "-projection-worker", "-projection-worker-drain-timeout=3s")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "ZBOARD_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "ZBOARD_TEST_PROJECTION_WORKER_CHILD=1")
	logPath := filepath.Join(dir, "worker.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			cancel()
			<-done
		}
	})
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var schedules int64
		if err := db.Table("job_schedules").Where("id IN ?", []string{application.UsageProjectionJobID, application.NodeObservationJobID}).Count(&schedules).Error; err != nil {
			t.Fatal(err)
		}
		if schedules == 2 {
			break
		}
		select {
		case err := <-done:
			finished = true
			out, _ := os.ReadFile(logPath)
			t.Fatalf("worker exited before registering projections: %v %s", err, out)
		case <-deadline.C:
			t.Fatal("worker did not register prepared projections")
		case <-tick.C:
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			out, _ := os.ReadFile(logPath)
			t.Fatalf("worker failed graceful shutdown: %v %s", err, out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit after drain")
	}
	var actual model.Node
	if err := db.First(&actual, node.ID).Error; err != nil || actual.NodeCredential != node.NodeCredential {
		t.Fatal("worker migrated node credentials", err)
	}
	var users int64
	if err := db.Model(&model.User{}).Count(&users).Error; err != nil || users != 0 {
		t.Fatal("worker bootstrapped administrator", err)
	}
	for _, name := range []string{c.ZeroEventSpoolDir, c.FileStorageDir} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatal("worker opened server-local storage", name, err)
		}
	}
}

func projectionWorkerTestConfig(path, dir string) cfgpkg.Config {
	c := cfgpkg.Config{Environment: cfgpkg.EnvironmentTest, DatabaseDriver: datastore.DriverSQLite, DataSource: path, DatabaseMaxOpenConnections: 1, DatabaseMaxIdleConnections: 1, DatabaseConnectionMaxLifetimeSecs: 3600, JwtSecret: "0123456789abcdef0123456789abcdef", CredentialEncryptionKey: strings.Repeat("0123456789abcdef", 4), BootstrapAdminEmail: "worker-admin@example.test", BootstrapAdminPassword: "only-test-bootstrap-password", ZeroEventSpoolDir: filepath.Join(dir, "must-not-open-spool"), FileStorageDir: filepath.Join(dir, "must-not-open-files")}
	c.Name, c.Host, c.Port = "projection-worker-test", "127.0.0.1", 18443
	return c
}

func TestProjectionWorkerProcessesRecoverCheckpointFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.db")
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
	user := model.User{Email: "worker-process@example.test", Password: "unused", Status: "active"}
	node := model.Node{Name: "worker-process", Config: "{}"}
	for _, row := range []any{&user, &node} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, RuntimeKey: uuid.NewString(), Name: "worker-process", Protocol: "vless", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	rows := make([]model.TrafficRecord, 201)
	for i := range rows {
		rows[i] = model.TrafficRecord{UserID: user.ID, NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: fmt.Sprintf("process-report-%d", i), Nonce: fmt.Sprintf("process-nonce-%d", i), RawBytes: 3, DownloadBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: now, Meta: "original diagnostic evidence"}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		return meteringstore.EnqueueUsageProjections(tx, rows)
	}); err != nil {
		t.Fatal(err)
	}
	var before []model.TrafficRecord
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_worker_checkpoint BEFORE DELETE ON metering_usage_projection_pending BEGIN SELECT RAISE(ABORT,'injected worker checkpoint failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	c := projectionWorkerTestConfig(path, dir)
	configPath := filepath.Join(dir, "worker.json")
	raw, err := json.Marshal(map[string]any{"Name": c.Name, "Host": c.Host, "Port": c.Port, "Log": map[string]any{"Level": "error", "Mode": "console"}, "environment": c.Environment, "database_driver": c.DatabaseDriver, "datasource": c.DataSource, "jwt_secret": c.JwtSecret, "credential_encryption_key": c.CredentialEncryptionKey, "zero_event_spool_dir": c.ZeroEventSpoolDir, "file_storage_dir": c.FileStorageDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	start := func(label string) func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProjectionWorkerEntrypoint$", "-f", configPath, "-projection-worker", "-projection-worker-drain-timeout=3s")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "ZBOARD_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "ZBOARD_TEST_PROJECTION_WORKER_CHILD=1")
		logPath := filepath.Join(dir, label+".log")
		log, err := os.Create(logPath)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			cancel()
			log.Close()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		finished := false
		t.Cleanup(func() {
			cancel()
			if !finished {
				<-done
			}
			log.Close()
		})
		return func() {
			t.Helper()
			if err := cmd.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				finished = true
				if err != nil {
					output, _ := os.ReadFile(logPath)
					t.Fatalf("%s did not drain: %v %s", label, err, output)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s drain did not finish", label)
			}
		}
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.NewTimer(12 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for !check() {
			select {
			case <-deadline.C:
				t.Fatal("separate worker did not reach expected checkpoint")
			case <-tick.C:
			}
		}
	}
	stopFirst := start("first")
	wait(func() bool {
		var n int64
		if err := db.Table("job_runs").Where("handler = ? AND attempt_count >= 1 AND state IN ?", application.UsageProjectionJobID, []string{"retry_wait", "failed"}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n > 0
	})
	var pending int64
	if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil || pending != 201 {
		t.Fatalf("failed checkpoint lost intent: %d %v", pending, err)
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("failed checkpoint committed partial %s: %d %v", table, n, err)
		}
	}
	stopSecond := start("second")
	stopFirst()
	if err := db.Exec("DROP TRIGGER reject_worker_checkpoint").Error; err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		return pending == 0
	})
	stopSecond()
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var result struct{ Used, Raw, Records int64 }
		if err := db.Table(table).Select("COALESCE(SUM(used_bytes),0) AS used,COALESCE(SUM(raw_bytes),0) AS raw,COALESCE(SUM(record_count),0) AS records").Scan(&result).Error; err != nil || result.Used != 1005 || result.Raw != 603 || result.Records != 201 {
			t.Fatalf("separate workers doubled or lost %s: %+v %v", table, result, err)
		}
	}
	var after []model.TrafficRecord
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil || string(afterJSON) != string(beforeJSON) {
		t.Fatal("separate projection process changed authoritative facts", err)
	}
	for _, name := range []string{c.ZeroEventSpoolDir, c.FileStorageDir} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatal("worker opened server-local storage", name, err)
		}
	}
}
