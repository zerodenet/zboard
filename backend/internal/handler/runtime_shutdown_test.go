package handler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestShutdownWaitsForExecutionBeforeReleasingSpoolAndRetainsPendingEvent(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.Storage.MinFreeSpace, cfg.Storage.EmergencyReserve = 0, 0
	spool, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := spool.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	runtime := &zeroEventRuntime{spool: spool, cancel: cancel, done: make(chan struct{})}
	zeroEventRuntimeRegistry.Store(h, runtime)
	t.Cleanup(func() { _ = h.CloseZeroEventSpool() })
	event := zeroevent.Envelope{ID: "retained-shutdown-event", NodeID: 4, Type: "stats.sampled", OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"active_sessions":1}`)}
	if err := spool.Append(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	h.startScheduledJob("shutdown_inflight", time.Hour, func(call context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-call.Done():
			return call.Err()
		}
	})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("local execution did not start")
	}
	shutdown := make(chan error, 1)
	go func() { shutdown <- h.Shutdown(t.Context()) }()
	admissionDeadline := time.NewTimer(3 * time.Second)
	defer admissionDeadline.Stop()
	for {
		err := h.services.Jobs.Register(jobs.Definition{ID: "shutdown_admission_probe", Handler: "shutdown_admission_probe", Timeout: time.Second}, func(context.Context, jobs.Run) error { return nil })
		if errors.Is(err, jobs.ErrRuntimeDraining) {
			break
		}
		if err != nil && !errors.Is(err, jobs.ErrConflict) {
			t.Fatal("shutdown interrupted before graceful admission fence", err)
		}
		select {
		case <-admissionDeadline.C:
			t.Fatal("shutdown did not stop admission")
		case <-time.After(time.Millisecond):
		}
	}
	// The in-flight handler is the latch: shutdown cannot release file ownership
	// before it completes, even though admission has already stopped.
	other, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Start(t.Context()); err == nil {
		_ = other.Close()
		t.Fatal("shutdown released spool ownership while execution remained active")
	}
	select {
	case err := <-shutdown:
		t.Fatalf("shutdown returned before in-flight execution completed: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish after execution release")
	}
	if err := other.Start(t.Context()); err != nil {
		t.Fatal("closed process retained file ownership", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	batch, err := other.ReadBatch(t.Context(), 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ID != event.ID {
		t.Fatal("pending durable event did not survive local shutdown", batch, err)
	}
}

func TestShutdownDrainDeadlineInterruptsOnlyLocalWorkAndPreservesUnknownResult(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	entered := make(chan struct{})
	h.startScheduledJob("shutdown_timeout", time.Hour, func(call context.Context) error {
		close(entered)
		<-call.Done()
		return call.Err()
	})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("local execution did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown lost drain deadline", err)
	}
	rows, err := jobstore.New(h.db).List(t.Context(), "system", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Handler == "shutdown_timeout" {
			if row.State != jobs.Unknown {
				t.Fatalf("interrupted task falsely finalized: %s", row.State)
			}
			return
		}
	}
	t.Fatal("interrupted durable task disappeared")
}

func TestSQLiteSpoolStartupFailurePreservesOtherProcessTasks(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertSpoolStartupFailurePreservesOtherProcessTasks(t, h)
}

func TestMySQLSpoolStartupFailurePreservesOtherProcessTasks(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN for isolated real MySQL")
	}
	assertSpoolStartupFailurePreservesOtherProcessTasks(t, newMySQLOrderFixture(t).h)
}

func assertSpoolStartupFailurePreservesOtherProcessTasks(t *testing.T, h *handlers) {
	t.Helper()
	ctx := t.Context()
	store := jobstore.New(h.db)
	active, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: "foreign-expiry", Handler: "credential_expiry", Resource: "foreign-spool-startup", Payload: `{"revision":"1"}`})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(ctx, "other-process", []string{"credential_expiry"}, time.Minute)
	if err != nil || lease.Run.ID != active.ID {
		t.Fatal("foreign expiry lease not acquired", lease, err)
	}
	pending, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: "foreign-history", Handler: "history_retention", Resource: "foreign-spool-startup", Payload: `{"revision":"1"}`})
	if err != nil {
		t.Fatal(err)
	}
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.Storage.MinFreeSpace, cfg.Storage.EmergencyReserve = 0, 0
	owner, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if err := h.ConfigureZeroEventSpool(cfg); err == nil {
		_ = h.CloseZeroEventSpool()
		t.Fatal("second process obtained an already-owned spool")
	}
	rows, err := store.List(ctx, "system", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[active.ID] != jobs.Running || states[pending.ID] != jobs.Queued {
		t.Fatalf("failed local startup revoked another process's tasks: active=%s queued=%s", states[active.ID], states[pending.ID])
	}
	if err := store.Finish(ctx, lease, jobs.Succeeded); err != nil {
		t.Fatal("foreign owner lost completion fence", err)
	}
}
