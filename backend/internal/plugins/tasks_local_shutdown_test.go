package plugins

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

type pluginLocalShutdownFixture struct {
	manager  *Manager
	store    *jobstore.Store
	active   jobs.Claim
	queued   jobs.Run
	pluginID string
	handler  string
}

func newPluginLocalShutdownFixture(t *testing.T) pluginLocalShutdownFixture {
	t.Helper()
	m, db, _ := testManager(t, nil)
	store := jobstore.New(db)
	const pluginID = "example.local-shutdown"
	const handler = "plugin:" + pluginID + ":sync"
	owner := "plugin:" + pluginID
	firstDue := time.Now().UTC().Add(-time.Second)
	submit := func(key string, due time.Time) jobs.Run {
		t.Helper()
		run, err := store.Submit(t.Context(), jobs.Submission{Owner: owner, Key: key, Handler: handler, Resource: owner, Payload: `{"revision":"1"}`, NotBefore: due})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	first := submit("foreign-active", firstDue)
	active, err := store.Claim(t.Context(), "surviving-host", []string{handler}, time.Minute)
	if err != nil || active.Run.ID != first.ID {
		t.Fatal("foreign host did not own the first lease", active, err)
	}
	queued := submit("foreign-queued", firstDue.Add(time.Millisecond))
	runtime := jobs.NewGatedRuntime(store, "closing-host", func() bool { return false }, nil, nil)
	t.Cleanup(runtime.Close)
	if err := runtime.Register(jobs.Definition{ID: handler, Handler: handler, Owner: owner, Revision: "1", Timeout: time.Second}, func(context.Context, jobs.Run) error { t.Error("closing host executed foreign work"); return nil }); err != nil {
		t.Fatal(err)
	}
	m.tasks.mu.Lock()
	m.tasks.runtime = runtime
	m.tasks.registered[handler] = "1"
	m.tasks.mu.Unlock()
	return pluginLocalShutdownFixture{manager: m, store: store, active: active, queued: queued, pluginID: pluginID, handler: handler}
}

func (f pluginLocalShutdownFixture) assertForeignWorkPreserved(t *testing.T) {
	t.Helper()
	rows, err := f.store.List(t.Context(), "plugin:"+f.pluginID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[f.active.Run.ID] != jobs.Running || states[f.queued.ID] != jobs.Queued {
		t.Fatalf("local plugin lifecycle revoked foreign work: active=%s queued=%s", states[f.active.Run.ID], states[f.queued.ID])
	}
	if err := f.store.Finish(t.Context(), f.active, jobs.Succeeded); err != nil {
		t.Fatal("foreign lease lost completion fence", err)
	}
	next, err := f.store.Claim(t.Context(), "surviving-host", []string{f.handler}, time.Minute)
	if err != nil || next.Run.ID != f.queued.ID {
		t.Fatal("surviving host could not claim pending work", next, err)
	}
	if err := f.store.Finish(t.Context(), next, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
}

func TestManagerClosePreservesForeignTaskLeaseAndIntent(t *testing.T) {
	f := newPluginLocalShutdownFixture(t)
	f.manager.Close()
	f.manager.Close()
	f.assertForeignWorkPreserved(t)
}

func TestManagerLostLeaseTaskSyncPreservesForeignTaskLeaseAndIntent(t *testing.T) {
	f := newPluginLocalShutdownFixture(t)
	f.manager.lost.Store(true)
	f.manager.syncTaskRegistrations(t.Context())
	f.assertForeignWorkPreserved(t)
}

func TestManagerLostLeaseSupervisorPreservesForeignTaskLeaseAndIntent(t *testing.T) {
	f := newPluginLocalShutdownFixture(t)
	// Another host owns a live lease, so this old supervisor cannot take over.
	if err := f.manager.db.Table("plugin_host_leases").Where("id = 1").Updates(map[string]any{"owner": "surviving-host", "expires_at": time.Now().UTC().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	f.manager.lost.Store(true)
	f.manager.superviseOnce(t.Context())
	if !f.manager.lost.Load() {
		t.Fatal("old host stole an unexpired lease")
	}
	f.assertForeignWorkPreserved(t)
}

func TestExplicitPluginTaskRevocationStillCancelsForeignWork(t *testing.T) {
	f := newPluginLocalShutdownFixture(t)
	if err := f.manager.deactivatePluginTasks(t.Context(), f.pluginID); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.List(t.Context(), "plugin:"+f.pluginID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[f.active.Run.ID] != jobs.CancelRequested || states[f.queued.ID] != jobs.Canceled {
		t.Fatalf("explicit authority revocation became local-only: active=%s queued=%s", states[f.active.Run.ID], states[f.queued.ID])
	}
}

func TestLostPluginHostDetachesHandlerOutsideManagerMutex(t *testing.T) {
	m, db, _ := testManager(t, nil)
	store := jobstore.New(db)
	const handler = "plugin:example.local-shutdown:local"
	run, err := store.Submit(t.Context(), jobs.Submission{Owner: "plugin:example.local-shutdown", Key: "local-execution", Handler: handler, Payload: `{"revision":"1"}`})
	if err != nil {
		t.Fatal(err)
	}
	runtime := jobs.NewRuntime(store, "old-host", nil, nil)
	t.Cleanup(runtime.Close)
	entered, handlerStopped, abort := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(abort)
	if err := runtime.Register(jobs.Definition{ID: handler, Handler: handler, Revision: "1", Timeout: 5 * time.Second}, func(ctx context.Context, _ jobs.Run) error {
		close(entered)
		<-ctx.Done()
		// Real TaskExecutor also needs the process mutex around its final
		// snapshot check. An implementation waiting under m.mu would deadlock.
		for !m.mu.TryLock() {
			select {
			case <-abort:
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		m.mu.Unlock()
		close(handlerStopped)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	m.tasks.mu.Lock()
	m.tasks.runtime = runtime
	m.tasks.registered[handler] = "1"
	m.tasks.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("local handler did not start")
	}
	if err := db.Table("plugin_host_leases").Where("id = 1").Updates(map[string]any{"owner": "new-host", "expires_at": time.Now().UTC().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	m.lost.Store(true)
	supervised := make(chan struct{})
	go func() { m.superviseOnce(t.Context()); close(supervised) }()
	select {
	case <-supervised:
	case <-time.After(time.Second):
		t.Fatal("lost host waited for a handler under manager mutex")
	}
	select {
	case <-handlerStopped:
	default:
		t.Fatal("process cleanup preceded local handler completion")
	}
	deadline := time.After(time.Second)
	for {
		rows, err := store.List(t.Context(), "plugin:example.local-shutdown", 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 && rows[0].ID == run.ID && rows[0].State == jobs.Unknown {
			break
		}
		select {
		case <-deadline:
			t.Fatal("local interruption did not preserve unknown-result classification", rows)
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := store.Claim(t.Context(), "new-host", []string{handler}, time.Second); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("uncertain local operation was replayed", err)
	}
}
