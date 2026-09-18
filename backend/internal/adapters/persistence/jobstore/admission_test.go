package jobstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestPendingAdmissionReservesCoreAndReplaysAcceptedIntent(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	in := jobs.Submission{Owner: "plugin:a", Key: "accepted", Handler: "plugin:a:work", Payload: `{}`}
	accepted, err := a.Submit(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]Record, 0, jobs.MaxPluginOwnerPending-1)
	for i := 1; i < jobs.MaxPluginOwnerPending; i++ {
		rows = append(rows, Record{ID: fmt.Sprintf("pending-%d", i), Owner: in.Owner, Key: fmt.Sprint(i), Handler: in.Handler, State: string(jobs.Queued), NotBefore: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	}
	if err := a.db.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	again, err := b.Submit(ctx, in)
	if err != nil || again.ID != accepted.ID {
		t.Fatal(again, err)
	}
	in.Key = "overflow"
	if run, err := b.Submit(ctx, in); !errors.Is(err, jobs.ErrBackpressure) || run.ID != "" {
		t.Fatal(run, err)
	}
	core := jobs.Submission{Owner: "system", Key: "core", Handler: "core.work", Payload: `{}`}
	if _, err := b.Submit(ctx, core); err != nil {
		t.Fatal(err)
	}
	claim, err := a.Claim(ctx, "worker", []string{in.Handler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Submit(ctx, in); err != nil {
		t.Fatal("slot not released on claim", err)
	}
	if err := a.Finish(ctx, claim, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalPendingAdmissionKeepsOneMaintenanceSlot(t *testing.T) {
	a, b := fixture(t, 1)
	rows := make([]Record, 0, jobs.MaxPending)
	for i := 0; i < jobs.MaxPending; i++ {
		rows = append(rows, Record{ID: fmt.Sprintf("pending-%d", i), Owner: "system", Key: fmt.Sprint(i), Handler: "core.work", State: string(jobs.Queued), NotBefore: time.Now(), CreatedAt: time.Now()})
	}
	if err := a.db.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	in := jobs.Submission{Owner: "system", Key: "new", Handler: "core.work", Payload: `{}`}
	if _, err := b.Submit(context.Background(), in); !errors.Is(err, jobs.ErrBackpressure) {
		t.Fatal(err)
	}
	in.Resource = jobs.MaintenanceResource
	if _, err := b.Submit(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Key = "second-maintenance"
	if _, err := b.Submit(context.Background(), in); !errors.Is(err, jobs.ErrBackpressure) {
		t.Fatal(err)
	}
}
