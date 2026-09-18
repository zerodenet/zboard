package application

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type maintenanceReadFixture struct {
	read func(context.Context) (platform.MaintenanceData, error)
}

func (s maintenanceReadFixture) Read(ctx context.Context) (platform.MaintenanceData, error) {
	return s.read(ctx)
}
func (maintenanceReadFixture) Apply(context.Context, uint, platform.MaintenanceUpdate) error {
	return nil
}
func (maintenanceReadFixture) Patch(context.Context, uint, platform.MaintenanceSetting) error {
	return nil
}

func TestMaintenanceCacheFailsClosedAndDoesNotPublishCallerCancellation(t *testing.T) {
	calls := 0
	unavailable := errors.New("database unavailable")
	readErr := unavailable
	app := &Services{Maintenance: platform.Maintenance{Repository: maintenanceReadFixture{read: func(context.Context) (platform.MaintenanceData, error) {
		calls++
		return platform.MaintenanceData{}, readErr
	}}}}
	app.StartWork()
	if !app.WorkPaused() || !app.WorkPaused() || calls != 1 {
		t.Fatal("outage not cached fail-closed", calls)
	}
	readErr = nil
	app.InvalidateMaintenance()
	if app.WorkPaused() || calls != 2 {
		t.Fatal("invalidation did not refresh", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Maintenance.Repository = maintenanceReadFixture{read: func(context.Context) (platform.MaintenanceData, error) {
		calls++
		cancel()
		return platform.MaintenanceData{}, ctx.Err()
	}}
	if _, err := app.MaintenanceState(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if app.WorkPaused() || calls != 3 {
		t.Fatal("caller cancellation poisoned global work gate", calls)
	}
	app.InvalidateMaintenance()
	app.Maintenance.Repository = maintenanceReadFixture{read: func(context.Context) (platform.MaintenanceData, error) {
		return platform.MaintenanceData{Enabled: true}, nil
	}}
	if !app.WorkPaused() {
		t.Fatal("maintenance did not pause work")
	}
}

func workGateFixture(t *testing.T) (*Services, *gorm.DB, uint) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ key, value string }{{"maintenance_enabled", "false"}, {"maintenance_title", "维护"}, {"maintenance_message", "等待"}, {"maintenance_task_id", "0"}} {
		if err := db.Create(&model.SystemConfig{ConfigKey: entry.key, Name: entry.key, Value: entry.value, ValueType: "string", Revision: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	user := model.User{Email: "work-gate@example.test", Password: "unused", Status: "active", IsAdmin: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	app := New(db, "test-secret")
	t.Cleanup(app.Close)
	return app, db, user.ID
}

type migrationGateTarget struct{}

func (migrationGateTarget) Check(context.Context, platform.MigrationTarget) error { return nil }

type migrationGateCipher struct{}

func (migrationGateCipher) Encrypt(string) (string, error) { return "protected-test-cipher", nil }

func TestPlatformMutationsInvalidateApplicationGateWithoutHTTP(t *testing.T) {
	app, _, actor := workGateFixture(t)
	app.StartWork()
	if app.WorkPaused() {
		t.Fatal("initial state paused")
	}
	revisions := map[string]uint64{"maintenance_enabled": 1, "maintenance_title": 1, "maintenance_message": 1}
	if err := app.Maintenance.Update(context.Background(), actor, platform.MaintenanceUpdate{Enabled: true, Title: "维护", Message: "检查", ExpectedRevisions: revisions}); err != nil {
		t.Fatal(err)
	}
	if !app.WorkPaused() {
		t.Fatal("internal update did not invalidate cache")
	}
	if err := app.Maintenance.Patch(context.Background(), actor, platform.MaintenanceSetting{Key: "maintenance_enabled", Value: "false"}); err != nil {
		t.Fatal(err)
	}
	if app.WorkPaused() {
		t.Fatal("internal patch did not invalidate cache")
	}
	admission := app.DatabaseMigration(migrationGateCipher{})
	admission.Targets = migrationGateTarget{}
	if _, err := admission.Start(context.Background(), actor, platform.MigrationStart{Target: platform.MigrationTarget{TargetDriver: "mysql", TargetDataSource: "test"}, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if !app.WorkPaused() {
		t.Fatal("migration admission did not invalidate cache")
	}
}

func TestBootstrapGateAlsoBlocksMaintenanceExecution(t *testing.T) {
	app, db, _ := workGateFixture(t)
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_enabled").Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	def := jobs.Definition{ID: "maintenance-test", Handler: "maintenance-test", Owner: "system", Resource: jobs.MaintenanceResource, Revision: "1", Timeout: time.Minute}
	if err := app.Jobs.RegisterMaintenance(def, func(context.Context, jobs.Run) error { close(started); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := jobstore.New(db).Submit(context.Background(), jobs.Submission{Owner: "system", Key: "bootstrap-maintenance", Handler: def.ID, Resource: jobs.MaintenanceResource, Payload: `{"revision":"1"}`}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("maintenance bypassed initialization")
	case <-time.After(1200 * time.Millisecond):
	}
	app.StartWork()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("ready application did not execute maintenance")
	}
	app.Close()
	app.StartWork()
	if !app.WorkPaused() {
		t.Fatal("closed application reopened bootstrap gate")
	}
}

func TestMaintenanceCacheCoalescesConcurrentReaders(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	app := &Services{Maintenance: platform.Maintenance{Repository: maintenanceReadFixture{read: func(ctx context.Context) (platform.MaintenanceData, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return platform.MaintenanceData{}, nil
		case <-ctx.Done():
			return platform.MaintenanceData{}, ctx.Err()
		}
	}}}}
	app.StartWork()
	results := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		go func() { results <- app.WorkPaused() }()
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("cache reader did not start")
	}
	close(release)
	for i := 0; i < 8; i++ {
		select {
		case paused := <-results:
			if paused {
				t.Fatal("successful read paused work")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cache reader stuck")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent readers bypassed cache", calls.Load())
	}
}
