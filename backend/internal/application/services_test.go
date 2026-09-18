package application

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
)

func TestApplicationOwnsWorkGateAndRuntimeShutdown(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	app := New(db, "test-key")
	defer app.Close()
	started, stopped := make(chan struct{}), make(chan struct{})
	d := jobs.Definition{ID: "test", Handler: "test", Owner: "system", Revision: "1", Timeout: time.Minute}
	if err := app.Jobs.Register(d, func(ctx context.Context, _ jobs.Run) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := jobstore.New(db).Submit(context.Background(), jobs.Submission{Owner: "system", Key: "test-one", Handler: "test", Payload: `{"revision":"1"}`}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("closed bootstrap gate allowed work")
	case <-time.After(1200 * time.Millisecond):
	}
	app.StartWork()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("open bootstrap gate did not admit work")
	}
	app.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("close returned before active work stopped")
	}
	if err := app.Jobs.Register(jobs.Definition{ID: "after", Handler: "after", Timeout: time.Second}, func(context.Context, jobs.Run) error { return nil }); err == nil {
		t.Fatal("closed application accepted a new handler")
	}
}
