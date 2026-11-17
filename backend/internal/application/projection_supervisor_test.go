//go:build linux

package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/testsupport/localworkerlock"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
)

type blockedProjectionResources struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func TestProjectionStorageObservationKeepsOutcomeAndClearsPanicOrCancellation(t *testing.T) {
	app := &Services{Jobs: &jobs.Runtime{}}
	if n, err := app.observeProjectionStorage(func() (int, error) { return 200, nil }); n != 200 || err != nil {
		t.Fatal(n, err)
	}
	sentinel := errors.New("storage failure")
	if n, err := app.observeProjectionStorage(func() (int, error) { return 7, sentinel }); n != 7 || err != sentinel {
		t.Fatal("storage outcome identity changed", n, err)
	}
	if _, err := app.observeProjectionStorage(func() (int, error) { return 0, context.Canceled }); err != context.Canceled {
		t.Fatal("cancellation hidden", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("projection storage panic was hidden")
			}
		}()
		app.observeProjectionStorage(func() (int, error) { panic("fixture storage panic") })
	}()
	p := app.Jobs.DatabasePressure(time.Now(), time.Minute)
	if p.StorageActive != 0 || p.StorageUnknown || !p.StorageFailed || p.Failed || p.Writes != 1 {
		t.Fatal("panic/cancel cleanup invented successful storage", p)
	}
}

func (s *blockedProjectionResources) Sample(now time.Time) localworkers.Resources {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return localworkers.Resources{At: now}
}

type neverLaunchProjectionChild struct{}

func (neverLaunchProjectionChild) Start(context.Context) (localworkers.Process, error) {
	panic("blocked resource observation launched a child")
}

func TestProjectionSupervisorCloseBudgetPreservesUnconfirmedOwnership(t *testing.T) {
	localworkerlock.Hold(t)
	dir := t.TempDir()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(dir, "unprepared.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	app := New(db, "")
	defer app.Close()
	app.StartWork()
	resources := &blockedProjectionResources{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(resources.release) })
	policy := localworkers.DefaultPolicy()
	policy.MaxWorkers = 1
	lock := filepath.Join(dir, "supervisor.lock")
	closeLocal, err := app.StartProjectionSupervisor(&ProjectionSupervisorOptions{Policy: policy, Interval: time.Second, DrainTimeout: time.Second, LockPath: lock, Launcher: neverLaunchProjectionChild{}, Resources: resources})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-resources.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("resource fixture did not enter")
	}
	closed := make(chan error, 1)
	go func() { closed <- closeLocal() }()
	select {
	case err := <-closed:
		if err == nil || !strings.Contains(err.Error(), "not confirmed") {
			t.Fatal("blocked observation was reported as a confirmed stop", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("blocked observation exceeded the supervisor's bounded close")
	}
	if other, err := localworkers.AcquireLock(lock); err == nil {
		other.Close()
		t.Fatal("unconfirmed close released its ownership")
	}
	release.Do(func() { close(resources.release) })
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if other, err := localworkers.AcquireLock(lock); err == nil {
			other.Close()
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("confirmed observer completion retained ownership")
		case <-tick.C:
		}
	}
}

func TestProjectionSupervisorDefaultAddsNoResourcesOrProcesses(t *testing.T) {
	localworkerlock.Hold(t)
	app := &Services{}
	if close, err := app.StartProjectionSupervisor(nil); close != nil || err != nil {
		t.Fatal("disabled supervision created resources", close != nil, err)
	}
	if close, err := app.StartProjectionSupervisor(&ProjectionSupervisorOptions{Policy: localworkers.DefaultPolicy(), LockPath: "/unavailable/default-must-not-open"}); close != nil || err != nil {
		t.Fatal("default policy opened a resource", close != nil, err)
	}
	owner, err := localworkers.AcquireLock(filepath.Join(t.TempDir(), "after-default.lock"))
	if err != nil {
		t.Fatal("disabled supervisor retained namespace ownership", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}
