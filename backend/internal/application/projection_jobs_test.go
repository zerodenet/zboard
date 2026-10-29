package application

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

type projectionUsageProbe struct {
	ready                    bool
	readyErr, runErr         error
	count                    int
	readyContext, runContext context.Context
}

func (p *projectionUsageProbe) HasPending(ctx context.Context) (bool, error) {
	p.readyContext = ctx
	return p.ready, p.readyErr
}
func (p *projectionUsageProbe) Drain(ctx context.Context, count int) (int, error) {
	p.runContext = ctx
	p.count = count
	return count, p.runErr
}

type projectionObservationProbe struct {
	ready                    bool
	readyErr, runErr         error
	count                    int
	bytes                    int64
	readyContext, runContext context.Context
}

func (p *projectionObservationProbe) HasPending(ctx context.Context) (bool, error) {
	p.readyContext = ctx
	return p.ready, p.readyErr
}
func (p *projectionObservationProbe) Drain(ctx context.Context, count int, bytes int64) (int, error) {
	p.runContext = ctx
	p.count, p.bytes = count, bytes
	return count, p.runErr
}

func TestProjectionJobDefinitionsPreserveExistingContract(t *testing.T) {
	usage := &projectionUsageProbe{ready: true}
	observation := &projectionObservationProbe{ready: true}
	app := &Services{UsageProjections: metering.UsageProjections{Repository: usage}, NodeObservations: network.ObservationProjections{Repository: observation}}
	for _, tc := range []struct {
		id, name string
		job      PeriodicJob
	}{
		{"usage_projection", "计量统计投影", app.UsageProjectionJob()},
		{"node_observation", "节点观察投影", app.NodeObservationJob()},
	} {
		t.Run(tc.id, func(t *testing.T) {
			job := tc.job
			if job.ID != tc.id || job.Name != tc.name || job.Interval != time.Second || job.Timeout != 30*time.Minute || !job.Immediate || job.External || !job.RetryFailures || !job.ReconcileAfterLoss {
				t.Fatal("projection scheduling policy changed", job)
			}
			if job.TimeZone != "" || job.MisfirePolicy != "" || !job.FirstPlannedAt.IsZero() {
				t.Fatal("former zero-valued planning policy changed")
			}
			if job.Ready == nil || job.Run == nil {
				t.Fatal("projection callbacks missing")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if pending, err := job.Ready(ctx); err != nil || !pending {
				t.Fatal(pending, err)
			}
			if err := job.Run(ctx); !errors.Is(err, jobs.ErrContinue) {
				t.Fatal("a committed full bounded batch must yield through the durable scheduler", err)
			}
			if tc.id == "usage_projection" {
				if usage.count != 200 || usage.readyContext != ctx || usage.runContext != ctx {
					t.Fatal("usage bounds/context changed")
				}
			} else if observation.count != 32 || observation.bytes != 8*1024*1024 || observation.readyContext != ctx || observation.runContext != ctx {
				t.Fatal("observation bounds/context changed")
			}
		})
	}
}
func TestProjectionDefinitionsKeepLiveCapabilityAndErrorSemantics(t *testing.T) {
	app := &Services{UsageProjections: metering.UsageProjections{Repository: &projectionUsageProbe{}}, NodeObservations: network.ObservationProjections{Repository: &projectionObservationProbe{}}}
	usageJob, observationJob := app.UsageProjectionJob(), app.NodeObservationJob()
	// Existing handler closures resolve the configured capability at invocation;
	// do not accidentally freeze an earlier repository via a method value.
	readyErr := errors.New("projection readiness unavailable")
	for _, err := range []error{nil, errors.New("projection retryable failure"), jobs.ErrContinue, errors.Join(jobs.ErrContinue, jobs.ErrUncertain), context.DeadlineExceeded} {
		usage := &projectionUsageProbe{ready: true, readyErr: readyErr, runErr: err}
		observation := &projectionObservationProbe{ready: true, readyErr: readyErr, runErr: err}
		app.UsageProjections.Repository = usage
		app.NodeObservations.Repository = observation
		for _, job := range []PeriodicJob{usageJob, observationJob} {
			if _, got := job.Ready(t.Context()); got != readyErr {
				t.Fatal("readiness error was hidden", got)
			}
			want := err
			if want == nil {
				want = jobs.ErrContinue // The probe reports a committed full batch.
			}
			if got := job.Run(t.Context()); got != want {
				t.Fatal("projection failure was masked or full-batch continuation lost", got)
			}
		}
	}
	app.UsageProjections.Repository = &projectionUsageProbe{ready: false}
	app.NodeObservations.Repository = &projectionObservationProbe{ready: false}
	for _, job := range []PeriodicJob{usageJob, observationJob} {
		if pending, err := job.Ready(t.Context()); pending || err != nil {
			t.Fatal("empty projection queue must suppress dispatch", pending, err)
		}
	}
}
func TestServicesAndProjectionDefinitionsDoNotPrepareOrRegister(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "unprepared.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	// Opening the test connection is setup. Construction/definition/Close must
	// execute no schema, authority, maintenance or ledger statement thereafter.
	statements := 0
	reject := func(tx *gorm.DB) { statements++; tx.AddError(errors.New("constructor attempted database access")) }
	const name = "test:projection_constructor_no_sql"
	if err := db.Callback().Query().Before("gorm:query").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	app := New(db, "")
	_ = app.UsageProjectionJob()
	_ = app.NodeObservationJob()
	if !app.WorkPaused() || len(app.Jobs.RegisteredJobs()) != 0 {
		t.Fatal("construction opened readiness or registered work")
	}
	app.Close()
	if statements != 0 {
		t.Fatal("construction or local close accessed the database", statements)
	}
	if err := db.Callback().Query().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Remove(name); err != nil {
		t.Fatal(err)
	}
	tables, err := db.Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		t.Fatal("constructor prepared the empty database", tables, err)
	}
}
