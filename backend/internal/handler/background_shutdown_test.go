package handler

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestBackgroundShutdownPreservesOtherWorkerLeaseAndQueuedIntent(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertBackgroundShutdownPreservesOtherWorkerLeaseAndQueuedIntent(t, h)
}

func TestMySQLBackgroundShutdownPreservesOtherWorkerLeaseAndQueuedIntent(t *testing.T) {
	assertBackgroundShutdownPreservesOtherWorkerLeaseAndQueuedIntent(t, newMySQLBusinessOrderFixture(t).h)
}

func assertBackgroundShutdownPreservesOtherWorkerLeaseAndQueuedIntent(t *testing.T, h *handlers) {
	t.Helper()
	ctx := t.Context()
	store := jobstore.New(h.db)
	const handler = "shutdown_shared"
	submit := func(key string) jobs.Run {
		t.Helper()
		run, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: key, Handler: handler, Resource: "core:" + handler, Payload: `{"revision":"1"}`})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	active := submit("other-worker-active")
	lease, err := store.Claim(ctx, "other-process", []string{handler}, time.Minute)
	if err != nil || lease.Run.ID != active.ID {
		t.Fatal("other process did not own first lease", lease, err)
	}
	queued := submit("surviving-queued-intent")
	// The foreign resource lease prevents the closing runtime from executing
	// either this pending intent or its own scheduled occurrence.
	h.startScheduledJob(handler, time.Hour, func(context.Context) error {
		t.Error("closing process executed a foreign-locked resource")
		return nil
	})
	h.CloseBackgroundJobs()
	rows, err := store.List(ctx, "system", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[active.ID] != jobs.Running || states[queued.ID] != jobs.Queued {
		t.Fatalf("process shutdown revoked shared work: active=%s queued=%s", states[active.ID], states[queued.ID])
	}
	if err := store.Finish(ctx, lease, jobs.Succeeded); err != nil {
		t.Fatal("foreign owner lost its completion fence", err)
	}
	next, err := store.Claim(ctx, "surviving-process", []string{handler}, time.Minute)
	if err != nil || next.Run.ID != queued.ID {
		t.Fatal("queued intent did not survive closing another runtime", next, err)
	}
	if err := store.Finish(ctx, next, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	// Ordinary idempotent shutdown remains local on a second invocation.
	h.CloseBackgroundJobs()
}
