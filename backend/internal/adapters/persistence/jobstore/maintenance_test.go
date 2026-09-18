package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaintenanceReservationExcludesOtherInstancesAndUnknownWork(t *testing.T) {
	a, b := fixture(t, 4)
	ctx := context.Background()
	if err := a.Schedule(ctx, jobs.Definition{ID: "reserved", Owner: "plugin:example", Handler: "reserved", Resource: jobs.MaintenanceResource, Interval: time.Second, Timeout: time.Second}); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal("periodic maintenance reservation accepted", err)
	}
	submit(t, a, "active", "ordinary", "")
	active, err := a.Claim(ctx, "a", []string{"ordinary"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	submit(t, a, "next", "ordinary", "")
	maintenance := submit(t, a, "maintenance", "maintenance", jobs.MaintenanceResource)
	if status, err := a.QueueStatus(ctx); err != nil || !status.MaintenanceReserved {
		t.Fatal("reservation missing from queue status", status, err)
	}
	for _, names := range [][]string{{"ordinary"}, {"maintenance"}} {
		if _, err := b.Claim(ctx, "b", names, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
			t.Fatal("exclusive reservation bypassed", err)
		}
	}
	if err := a.Finish(ctx, active, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim(ctx, "b", []string{"maintenance"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("unknown work ignored", err)
	}
	if err := a.Resolve(ctx, active.Run.ID, jobs.Failed); err != nil {
		t.Fatal(err)
	}
	lease, err := b.Claim(ctx, "b", []string{"maintenance"}, time.Minute)
	if err != nil || lease.Run.ID != maintenance.ID {
		t.Fatal(lease, err)
	}
	if _, err := a.Claim(ctx, "a", []string{"ordinary"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("ordinary work overlapped", err)
	}
	if err := b.Finish(ctx, lease, jobs.Unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Claim(ctx, "a", []string{"ordinary"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal("unknown maintenance lost exclusion", err)
	}
	if err := b.Resolve(ctx, lease.Run.ID, jobs.Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Claim(ctx, "a", []string{"ordinary"}, time.Minute); err != nil {
		t.Fatal("resolved maintenance still blocks", err)
	}
	if _, err := a.Submit(ctx, jobs.Submission{Owner: "plugin:example", Key: "escape", Handler: "plugin-task", Resource: jobs.MaintenanceResource, Payload: "{}"}); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal("plugin reserved host maintenance", err)
	}
}
func TestMaintenanceRuntimeUsesSharedWorkersWhilePaused(t *testing.T) {
	store, _ := fixture(t, 4)
	var paused atomic.Bool
	paused.Store(true)
	runtime := jobs.NewRuntime(store, "worker", paused.Load, nil)
	defer runtime.Close()
	ordinary := make(chan struct{}, 1)
	maintenance := make(chan error, 1)
	d := jobs.Definition{ID: "ordinary", Handler: "ordinary", Owner: "system", Revision: "1", Timeout: 5 * time.Second}
	if err := runtime.Register(d, func(context.Context, jobs.Run) error { ordinary <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	d.ID = "maintenance"
	d.Handler = d.ID
	d.Resource = jobs.MaintenanceResource
	if err := runtime.RegisterMaintenance(d, func(ctx context.Context, _ jobs.Run) error {
		select {
		case <-ctx.Done():
			maintenance <- ctx.Err()
			return ctx.Err()
		case <-time.After(1300 * time.Millisecond):
			maintenance <- nil
			return nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	d.ID = "plugin"
	d.Handler = d.ID
	d.Owner = "plugin:example"
	if err := runtime.RegisterMaintenance(d, func(context.Context, jobs.Run) error { return nil }); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal("plugin maintenance registration accepted", err)
	}
	for _, in := range []jobs.Submission{
		{Owner: "system", Key: "ordinary", Handler: "ordinary", Payload: `{"revision":"1"}`},
		{Owner: "system", Key: "maintenance", Handler: "maintenance", Resource: jobs.MaintenanceResource, Payload: `{"revision":"1"}`},
	} {
		if _, err := store.Submit(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-maintenance:
		if err != nil {
			t.Fatal("maintenance canceled by pause", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("maintenance was not admitted")
	}
	select {
	case <-ordinary:
		t.Fatal("ordinary ran during pause")
	default:
	}
	paused.Store(false)
	select {
	case <-ordinary:
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary work did not resume")
	}
}
