package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The managed process test executes the real main/flags/prepared worker path in
// this exact test executable, without launching the rest of the test suite.
func TestMain(m *testing.M) {
	if os.Getenv("ZBOARD_TEST_MANAGED_PROJECTION_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProjectionSupervisorRejectsOrphanOptionsAndMaintenanceBeforeConfiguration(t *testing.T) {
	originalMax := *projectionWorkersMax
	defer func() { *projectionWorkersMax = originalMax }()
	*projectionWorkersMax = 0
	flags := flag.NewFlagSet("supervisor", flag.ContinueOnError)
	flags.Bool("projection-workers-confirmed-benefit", false, "fixture")
	if err := flags.Parse([]string{"-projection-workers-confirmed-benefit=false"}); err != nil {
		t.Fatal(err)
	}
	if err := validateProjectionSupervisorFlags(flags, false); err == nil {
		t.Fatal("explicit default-valued orphan option started ordinary server")
	}
	*projectionWorkersMax = 1
	if err := validateProjectionSupervisorFlags(flag.NewFlagSet("supervisor", flag.ContinueOnError), true); err == nil {
		t.Fatal("maintenance admitted a local supervisor")
	}
	*projectionWorkersMax = 0
	if options, err := configuredProjectionSupervisor(projectionWorkerTestConfig("missing.db", t.TempDir())); err != nil || options != nil {
		t.Fatal("default server added supervision", options, err)
	}
}

func TestProjectionSupervisorManagedChildEntrypointAndDrain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("managed process ownership requires Linux deployment")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "managed.db")
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, dbPath)
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
	user := model.User{Email: "managed-worker@example.test", Password: "unused", Status: "active"}
	node := model.Node{Name: "managed-worker", Config: "{}"}
	for _, row := range []any{&user, &node} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, RuntimeKey: uuid.NewString(), Name: "managed-worker", Protocol: "vless", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	facts := make([]model.TrafficRecord, 201)
	for i := range facts {
		facts[i] = model.TrafficRecord{UserID: user.ID, NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: fmt.Sprintf("managed-%d", i), Nonce: fmt.Sprintf("managed-nonce-%d", i), RawBytes: 3, DownloadBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: now, Meta: "preserved original proof"}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&facts, 100).Error; err != nil {
			return err
		}
		return meteringstore.EnqueueUsageProjections(tx, facts)
	}); err != nil {
		t.Fatal(err)
	}
	var original []model.TrafficRecord
	if err := db.Order("id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := projectionWorkerTestConfig(dbPath, dir)
	c.Port = listener.Addr().(*net.TCPAddr).Port
	configuration := filepath.Join(dir, "managed.json")
	raw, err := json.Marshal(map[string]any{"Name": c.Name, "Host": c.Host, "Port": c.Port, "Log": map[string]any{"Level": "error", "Mode": "console"}, "environment": c.Environment, "database_driver": c.DatabaseDriver, "datasource": c.DataSource, "jwt_secret": c.JwtSecret, "credential_encryption_key": c.CredentialEncryptionKey, "zero_event_spool_dir": c.ZeroEventSpoolDir, "file_storage_dir": c.FileStorageDir})
	if err != nil || os.WriteFile(configuration, raw, 0600) != nil {
		t.Fatal("fixture configuration unavailable", err)
	}
	t.Setenv("ZBOARD_TEST_MANAGED_PROJECTION_MAIN", "1")
	launcher, err := localworkers.NewCommandLauncher(os.Args[0], configuration, 2*time.Second, 256<<20)
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(dir, "managed.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	launcher.Stdout, launcher.Stderr = logFile, logFile
	ownership, err := localworkers.AcquireLock(filepath.Join(dir, "owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ownership.Close()
	launcher.Ownership = ownership
	policy := localworkers.DefaultPolicy()
	policy.MaxWorkers, policy.AdministratorConfirmedBenefit, policy.MinHealthySamples = 1, true, 2
	policy.Cooldown, policy.IdleFor = 10*time.Second, 10*time.Second
	// Deliberately controlled, explicitly confirmed fixture observations test
	// process management. They are not claimed as host/DB capacity measurements.
	controller := &localworkers.Supervisor{Policy: policy, Interval: time.Second, DrainTimeout: 2 * time.Second, Launcher: launcher, Observe: func(_ context.Context, at time.Time) localworkers.Observation {
		return localworkers.Observation{At: at, QueueKnown: true, Pending: true, OldestAge: time.Minute, Resources: localworkers.Resources{At: at, Known: true, CPUFraction: .1, CPUCapacity: 4, AvailableMemoryBytes: 4 << 30, MinFreeDiskBytes: 4 << 30}, DatabaseAt: at, DatabaseWrites: 8, DatabaseLatency: time.Millisecond, DatabaseCapacityKnown: true, DatabaseCapacityAvailable: true, ProjectionActivityKnown: true}
	}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
	})
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var pending int64
		if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		select {
		case err := <-done:
			finished = true
			t.Fatalf("managed controller stopped before projection recovery: %v", err)
		case <-deadline.C:
			out, _ := os.ReadFile(filepath.Join(dir, "managed.log"))
			t.Fatalf("managed actual worker did not settle projections: %s", out)
		case <-tick.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed actual worker did not drain")
	}
	var after []model.TrafficRecord
	if err := db.Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(original, after) {
		t.Fatal("managed capacity assistance changed settlement facts", err)
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var total struct{ Raw, Used, Records int64 }
		if err := db.Table(table).Select("SUM(raw_bytes) AS raw, SUM(used_bytes) AS used, SUM(record_count) AS records").Scan(&total).Error; err != nil || total.Raw != 603 || total.Used != 1005 || total.Records != 201 {
			t.Fatal("managed process lost or duplicated projection", table, total, err)
		}
	}
	for _, path := range []string{c.ZeroEventSpoolDir, c.FileStorageDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("managed worker opened main-only local storage", path, err)
		}
	}
}

func TestProjectionSupervisorDoesNotInventLocalStorageForRemoteDatabase(t *testing.T) {
	for _, test := range []struct {
		driver, source string
		known          bool
	}{
		{"sqlite", "/data/zboard.db", true},
		{"sqlite", ":memory:", false},
		{"sqlite", "file::memory:?cache=shared", false},
		{"sqlite", "file:shared?mode=%6demory", false},
		{"mysql", "fixture@tcp(127.0.0.1:3306)/fixture", true},
		{"mysql", "fixture@tcp([::1]:3306)/fixture", true},
		{"mysql", "fixture@unix(/var/run/mysql.sock)/fixture", true},
		{"mysql", "fixture@tcp(database.example.test:3306)/fixture", false},
		{"mysql", "fixture@tcp(10.0.0.1:3306)/fixture", false},
	} {
		if got := locallyObservableProjectionDatabase(cfgpkg.Config{DatabaseDriver: test.driver, DataSource: test.source}); got != test.known {
			t.Fatal("database storage observation topology", test.driver, test.source, got)
		}
	}
}
