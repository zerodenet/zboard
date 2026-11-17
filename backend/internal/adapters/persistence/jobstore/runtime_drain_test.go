package jobstore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func TestSQLiteRuntimeDrainPreservesInFlightAndPendingWork(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertRuntimeDrainPreservesInFlightAndPendingWork(t)
}

func TestMySQLRuntimeDrainPreservesInFlightAndPendingWork(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN for isolated real MySQL")
	}
	assertRuntimeDrainPreservesInFlightAndPendingWork(t)
}

func TestSQLiteRuntimeObservesCancellationFromOtherStore(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertRuntimeObservesCancellationFromOtherStore(t)
}

func TestMySQLRuntimeObservesCancellationFromOtherStore(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN for isolated real MySQL")
	}
	assertRuntimeObservesCancellationFromOtherStore(t)
}

func assertRuntimeObservesCancellationFromOtherStore(t *testing.T) {
	t.Helper()
	a, b := fixture(t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const handler = "remote-cancellation-proof"
	run, err := a.Submit(ctx, jobs.Submission{Owner: "system", Key: handler, Handler: handler, Resource: handler, Payload: `{"revision":"1"}`})
	if err != nil {
		t.Fatal(err)
	}
	entered, stopped := make(chan struct{}), make(chan struct{})
	r := jobs.NewRuntime(a, "observing-process", nil, nil)
	defer r.Close()
	if err := r.Register(jobs.Definition{ID: handler, Handler: handler, Revision: "1", Timeout: 4 * time.Second}, func(call context.Context, _ jobs.Run) error {
		close(entered)
		<-call.Done()
		close(stopped)
		return call.Err()
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("handler did not begin", ctx.Err())
	}
	// A separate Store writes the durable cancellation; no local CancelActive
	// call or shared in-memory registration can interrupt this handler.
	if err := b.CancelHandler(ctx, handler); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not observe another store's durable cancellation")
	}
	if err := r.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := b.List(ctx, "system", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == run.ID {
			if row.State != jobs.Unknown {
				t.Fatalf("canceled execution lost uncertain-result classification: %s", row.State)
			}
			return
		}
	}
	t.Fatal("durable execution disappeared")
}

func assertRuntimeDrainPreservesInFlightAndPendingWork(t *testing.T) {
	a, b := fixture(t, 4)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const handler = "drain-proof"
	firstDue := time.Now().UTC().Add(-time.Second)
	add := func(key string) jobs.Run {
		t.Helper()
		due := firstDue
		if key == "second" {
			due = due.Add(time.Millisecond)
		}
		run, err := a.Submit(ctx, jobs.Submission{Owner: "system", Key: key, Handler: handler, Resource: "shared-resource", Payload: `{"revision":"1"}`, NotBefore: due})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	first, second := add("first"), add("second")
	entered, release, wasCanceled := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	r := jobs.NewRuntime(a, "draining-process", nil, nil)
	defer r.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	err := r.Register(jobs.Definition{ID: handler, Handler: handler, Revision: "1", Timeout: 3 * time.Second}, func(call context.Context, run jobs.Run) error {
		if run.ID != first.ID {
			return errors.New("pending work reached draining runtime")
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-call.Done():
			wasCanceled <- struct{}{}
			return call.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no initial execution", ctx.Err())
	}
	wait, stopWait := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stopWait()
	if err := r.Drain(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("drain did not respect wait budget", err)
	}
	select {
	case <-wasCanceled:
		t.Fatal("drain timeout canceled in-flight work")
	default:
	}
	close(release)
	if err := r.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := b.List(ctx, "system", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[first.ID] != jobs.Succeeded || states[second.ID] != jobs.Queued {
		t.Fatalf("drain changed durable outcome: first=%s second=%s", states[first.ID], states[second.ID])
	}
	lease, err := b.Claim(ctx, "surviving-process", []string{handler}, time.Second)
	if err != nil || lease.Run.ID != second.ID {
		t.Fatal("pending intent did not survive", lease, err)
	}
	if err := b.Finish(ctx, lease, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
}
