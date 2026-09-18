package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"testing"
	"time"
)

func TestRegisteredRequestsUseSharedQueueAndFenceRevision(t *testing.T) {
	store, other := fixture(t, 1)
	ctx := context.Background()
	runtime := jobs.NewGatedRuntime(store, "registered-test", func() bool { return false }, nil, nil)
	defer runtime.Close()
	d := jobs.Definition{ID: "plugin:example:refresh", Owner: "plugin:example", Handler: "plugin:example:refresh", Resource: "plugin:example", Revision: "generation-1", Timeout: time.Second}
	if err := runtime.Register(d, func(context.Context, jobs.Run) error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := runtime.RequestRegistered(ctx, d.Owner, d.ID, d.Revision, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := runtime.RequestRegistered(ctx, d.Owner, d.ID, d.Revision, "request-1")
	if err != nil || repeated.ID != first.ID {
		t.Fatalf("idempotency: %+v %v", repeated, err)
	}
	if _, err = runtime.RequestRegistered(ctx, "plugin:other", d.ID, d.Revision, "request-2"); !errors.Is(err, jobs.ErrConflict) {
		t.Fatalf("owner: %v", err)
	}
	if _, err = runtime.RequestRegistered(ctx, d.Owner, d.ID, "old-generation", "request-2"); !errors.Is(err, jobs.ErrConflict) {
		t.Fatalf("revision: %v", err)
	}
	core := submit(t, store, "core-work", "core-handler", "")
	claim, err := other.Claim(ctx, "worker", []string{d.ID}, time.Minute)
	if err != nil || claim.Run.ID != first.ID {
		t.Fatalf("shared queue: %+v %v", claim, err)
	}
	if _, err = other.Claim(ctx, "another", []string{core.Handler}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatalf("global budget bypass: %v", err)
	}
	if err = other.Finish(ctx, claim, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	pending, err := runtime.RequestRegistered(ctx, d.Owner, d.ID, d.Revision, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Deactivate(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.RequestRegistered(ctx, d.Owner, d.ID, d.Revision, "after-disable"); !errors.Is(err, jobs.ErrConflict) {
		t.Fatalf("deactivated: %v", err)
	}
	rows, err := runtime.OwnerRuns(ctx, d.Owner, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, run := range rows {
		if run.Owner != d.Owner {
			t.Fatal("foreign task")
		}
		if run.ID == pending.ID {
			found = true
			if run.State != jobs.Canceled {
				t.Fatalf("pending after deactivate: %s", run.State)
			}
		}
	}
	if !found {
		t.Fatal("missing persisted task")
	}
}
