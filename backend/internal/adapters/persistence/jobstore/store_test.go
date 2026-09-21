package jobstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
)

func fixture(t *testing.T, capacity int) (*Store, *Store) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") != "" {
		return mysqlFixture(t, capacity)
	}
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.db")
	a, err := datastore.OpenWithDriver("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := datastore.OpenWithDriver("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*Store{New(a), New(b)} {
		sql, _ := db.db.DB()
		t.Cleanup(func() { sql.Close() })
	}
	if err := a.AutoMigrate(&Budget{}, &ExecutionGroup{}, &DispatchLane{}, &Record{}, &Attempt{}, &Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Create(&Budget{ID: 1, Capacity: capacity}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Create(&ExecutionGroup{ID: "external", Capacity: 3}).Error; err != nil {
		t.Fatal(err)
	}
	return New(a), New(b)
}
func submit(t *testing.T, s *Store, key, handler, resource string) jobs.Run {
	t.Helper()
	r, err := s.Submit(context.Background(), jobs.Submission{Owner: "system", Key: key, Handler: handler, Resource: resource, Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIntentSurvivesNewStoreAndRejectsConflictingReplay(t *testing.T) {
	a, b := fixture(t, 2)
	ctx := context.Background()
	in := jobs.Submission{Owner: "plugin:a", Key: "invoice-1", Handler: "plugin.send", Payload: `{"amount":1}`}
	x, err := a.Submit(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.Submit(ctx, in)
	if err != nil || x.ID != y.ID {
		t.Fatal(x, y, err)
	}
	in.Payload = `{"amount":2}`
	if _, err = b.Submit(ctx, in); !errors.Is(err, jobs.ErrConflict) {
		t.Fatal(err)
	}
	rows, err := b.List(ctx, "plugin:b", 25, 0)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	rows, err = b.List(ctx, "plugin:a", 25, 0)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}

func TestResourceFenceTracksOnlyUnresolvedExecution(t *testing.T) {
	store, _ := fixture(t, 2)
	run := submit(t, store, "cleanup-node-1", "node_cleanup", "node-cleanup:target")
	for _, state := range []jobs.State{jobs.Queued, jobs.RetryWait, jobs.Running, jobs.CancelRequested, jobs.Unknown} {
		if err := store.db.Model(&Record{}).Where("id = ?", run.ID).Update("state", state).Error; err != nil {
			t.Fatal(err)
		}
		pending, err := store.HasUnresolvedResource(context.Background(), "node-cleanup:target")
		if err != nil || !pending {
			t.Fatalf("state=%s pending=%v err=%v", state, pending, err)
		}
	}
	for _, state := range []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Canceled} {
		if err := store.db.Model(&Record{}).Where("id = ?", run.ID).Update("state", state).Error; err != nil {
			t.Fatal(err)
		}
		pending, err := store.HasUnresolvedResource(context.Background(), "node-cleanup:target")
		if err != nil || pending {
			t.Fatalf("state=%s pending=%v err=%v", state, pending, err)
		}
	}
}

func TestDispatchMetadataDoesNotBreakLegacyIntentFingerprint(t *testing.T) {
	store, _ := fixture(t, 1)
	in := jobs.Submission{Owner: "system", Key: "legacy-fingerprint", Handler: "handler", Payload: `{}`}
	run, err := store.Submit(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var row Record
	if err := store.db.First(&row, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"MaxAttempts":1,"Owner":"system","Key":"legacy-fingerprint","Handler":"handler","Resource":"","ExecutionGroup":"","Payload":"{}","NotBefore":"0001-01-01T00:00:00Z"}`)
	sum := sha256.Sum256(legacy)
	if row.Fingerprint != hex.EncodeToString(sum[:]) || row.DispatchLane != "core" {
		t.Fatal(row.Fingerprint, row.DispatchLane)
	}
}

func TestDifferentWorkersShareOneGlobalBudget(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	submit(t, a, "core", "core.cleanup", "")
	submit(t, a, "plugin", "plugin.sync", "")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	claims := make(chan jobs.Claim, 2)
	for i, s := range []*Store{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Claim(ctx, []string{"a", "b"}[i], []string{"core.cleanup", "plugin.sync"}, time.Minute)
			results <- err
			if err == nil {
				claims <- c
			}
		}()
	}
	wg.Wait()
	close(results)
	ok, empty := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, jobs.ErrEmpty) {
			empty++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || empty != 1 {
		t.Fatal(ok, empty)
	}
	c := <-claims
	if err := a.Finish(ctx, c, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim(ctx, "b", []string{"core.cleanup", "plugin.sync"}, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredExecutionIsUnknownAndFencesLateCompletion(t *testing.T) {
	a, b := fixture(t, 2)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	b.now = func() time.Time { return now }
	submit(t, a, "first", "send", "plugin:a")
	now = now.Add(time.Millisecond)
	submit(t, a, "second", "send", "plugin:a")
	now = now.Add(time.Millisecond)
	submit(t, a, "other", "send", "plugin:b")
	c, err := a.Claim(ctx, "old", []string{"send"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	next, err := b.Claim(ctx, "new", []string{"send"}, time.Minute)
	if err != nil || next.Run.Resource != "plugin:b" {
		t.Fatal(next, err)
	}
	if err := a.Finish(ctx, c, jobs.Succeeded); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal(err)
	}
	var r Record
	if err := a.db.First(&r, "id = ?", c.Run.ID).Error; err != nil || r.State != string(jobs.Unknown) {
		t.Fatal(r, err)
	}
	if _, err := b.Claim(ctx, "new", []string{"send"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
}

func TestCoreAndPluginHandlersUseSameExecutionContract(t *testing.T) {
	a, b := fixture(t, 2)
	ctx := context.Background()
	submit(t, a, "core", "core.cleanup", "")
	submit(t, a, "plugin", "plugin.sync", "")
	seen := map[string]bool{}
	e := jobs.Executor{Store: b, Worker: "host-1", Timeout: time.Minute, Handlers: map[string]jobs.Handler{
		"core.cleanup": func(_ context.Context, r jobs.Run) error { seen[r.Handler] = true; return nil },
		"plugin.sync": func(_ context.Context, r jobs.Run) error {
			seen[r.Handler] = true
			return errors.New("provider failure")
		},
	}}
	_ = e.RunOne(ctx)
	_ = e.RunOne(ctx)
	if len(seen) != 2 {
		t.Fatal(seen)
	}
	rows, err := a.List(ctx, "system", 25, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		want := jobs.Succeeded
		if r.Handler == "plugin.sync" {
			want = jobs.Failed
		}
		if r.State != want {
			t.Fatal(r)
		}
	}
}

func TestPanicRecordsUncertainResult(t *testing.T) {
	a, _ := fixture(t, 1)
	submit(t, a, "panic", "panic", "")
	e := jobs.Executor{Store: a, Worker: "host", Timeout: time.Minute, Handlers: map[string]jobs.Handler{"panic": func(context.Context, jobs.Run) error { panic("private output") }}}
	if e.RunOne(context.Background()) == nil {
		t.Fatal("panic ignored")
	}
	rows, err := a.List(context.Background(), "system", 25, 0)
	if err != nil || rows[0].State != jobs.Unknown {
		t.Fatal(rows, err)
	}
}

func TestExplicitRetryCreatesOrderedAttemptsAndBacksOff(t *testing.T) {
	a, _ := fixture(t, 1)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	_, err := a.Submit(ctx, jobs.Submission{Owner: "system", Key: "retry", Handler: "retry", Payload: `{}`, MaxAttempts: 3, RetryBackoff: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	executor := jobs.Executor{Store: a, Worker: "host", Timeout: time.Minute, Handlers: map[string]jobs.Handler{
		"retry": func(context.Context, jobs.Run) error {
			calls++
			if calls < 3 {
				return jobs.Retry(errors.New("temporary provider failure"))
			}
			return nil
		},
	}}
	if err := executor.RunOne(ctx); err == nil {
		t.Fatal("first retryable failure was hidden")
	}
	rows, err := a.List(ctx, "system", 10, 0)
	if err != nil || len(rows) != 1 || rows[0].State != jobs.RetryWait || rows[0].Attempt != 1 || rows[0].MaxAttempts != 3 {
		t.Fatal(rows, err)
	}
	if err := executor.RunOne(ctx); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatalf("backoff did not delay retry: %v", err)
	}
	now = now.Add(100 * time.Millisecond)
	if err := executor.RunOne(ctx); err == nil {
		t.Fatal("second retryable failure was hidden")
	}
	now = now.Add(199 * time.Millisecond)
	if err := executor.RunOne(ctx); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatalf("exponential backoff did not delay retry: %v", err)
	}
	now = now.Add(time.Millisecond)
	if err := executor.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err = a.List(ctx, "system", 10, 0)
	if err != nil || rows[0].State != jobs.Succeeded || rows[0].Attempt != 3 {
		t.Fatal(rows, err)
	}
	var attempts []Attempt
	if err := a.db.Order("attempt_number").Find(&attempts, "run_id = ?", rows[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 || attempts[0].Number != 1 || attempts[1].Number != 2 || attempts[2].Number != 3 {
		t.Fatal(attempts)
	}
}

func TestRetryBudgetExhaustionIsTerminalFailure(t *testing.T) {
	a, _ := fixture(t, 1)
	now := time.Now().UTC().Truncate(time.Millisecond)
	a.now = func() time.Time { return now }
	_, err := a.Submit(context.Background(), jobs.Submission{Owner: "system", Key: "exhaust", Handler: "retry", Payload: `{}`, MaxAttempts: 2, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	executor := jobs.Executor{Store: a, Worker: "host", Timeout: time.Minute, Handlers: map[string]jobs.Handler{"retry": func(context.Context, jobs.Run) error { return jobs.Retry(errors.New("still unavailable")) }}}
	_ = executor.RunOne(context.Background())
	now = now.Add(time.Millisecond)
	_ = executor.RunOne(context.Background())
	rows, err := a.List(context.Background(), "system", 10, 0)
	if err != nil || rows[0].State != jobs.Failed || rows[0].Attempt != 2 || rows[0].FinishedAt == nil {
		t.Fatal(rows, err)
	}
	if err := executor.RunOne(context.Background()); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatalf("exhausted job was replayed: %v", err)
	}
}

func TestCrossInstanceCancellationPollAndAttemptHistory(t *testing.T) {
	a, b := fixture(t, 1)
	run := submit(t, a, "cancel", "cancel", "")
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		executor := jobs.Executor{Store: b, Worker: "remote-host", Timeout: time.Minute, Handlers: map[string]jobs.Handler{
			"cancel": func(ctx context.Context, _ jobs.Run) error {
				close(started)
				<-ctx.Done()
				return jobs.ErrCanceled
			},
		}}
		done <- executor.RunOne(context.Background())
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := a.CancelHandler(context.Background(), "cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, jobs.ErrCanceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remote cancellation was not observed")
	}
	rows, err := a.List(context.Background(), "system", 10, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != run.ID || rows[0].State != jobs.Canceled {
		t.Fatal(rows, err)
	}
	attempts, total, err := a.Attempts(context.Background(), run.ID, 25, 0)
	if err != nil || total != 1 || len(attempts) != 1 || attempts[0].Number != 1 || attempts[0].Worker != "remote-host" || attempts[0].State != jobs.Canceled || attempts[0].FinishedAt == nil {
		t.Fatal(attempts, total, err)
	}
}

func TestCancellationWithUncertainHandlerOutcomeBecomesUnknown(t *testing.T) {
	a, _ := fixture(t, 1)
	submit(t, a, "uncertain-cancel", "uncertain-cancel", "")
	claim, err := a.Claim(context.Background(), "host", []string{"uncertain-cancel"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CancelHandler(context.Background(), "uncertain-cancel"); err != nil {
		t.Fatal(err)
	}
	if err := a.Finish(context.Background(), claim, jobs.Failed); err != nil {
		t.Fatal(err)
	}
	rows, err := a.List(context.Background(), "system", 10, 0)
	if err != nil || rows[0].State != jobs.Unknown {
		t.Fatal(rows, err)
	}
}

func TestForgedCompletionAndFutureJobsDoNotConsumeBudget(t *testing.T) {
	a, b := fixture(t, 1)
	ctx := context.Background()
	_, err := a.Submit(ctx, jobs.Submission{Owner: "system", Key: "future", Handler: "work", Payload: `{}`, NotBefore: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Claim(ctx, "host", []string{"work"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
	submit(t, a, "ready", "work", "")
	c, err := b.Claim(ctx, "host", []string{"work"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	forged := c
	forged.Token = "wrong"
	if err = a.Finish(ctx, forged, jobs.Succeeded); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal(err)
	}
	if err = a.Finish(ctx, c, jobs.Succeeded); err != nil {
		t.Fatal(err)
	}
	if err = a.Finish(ctx, c, jobs.Failed); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal(err)
	}
}

func TestExternalWorkLeavesAdmissionCapacityForCoreProcessing(t *testing.T) {
	a, _ := fixture(t, 4)
	ctx := context.Background()
	for _, key := range []string{"a", "b", "c", "d"} {
		if _, err := a.Submit(ctx, jobs.Submission{Owner: "system", Key: key, Handler: "slow", ExecutionGroup: "external", Payload: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := a.Claim(ctx, "host", []string{"slow"}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Claim(ctx, "host", []string{"slow"}, time.Minute); !errors.Is(err, jobs.ErrEmpty) {
		t.Fatal(err)
	}
	submit(t, a, "accounting", "accounting", "")
	c, err := a.Claim(ctx, "host", []string{"slow", "accounting"}, time.Minute)
	if err != nil || c.Run.Handler != "accounting" {
		t.Fatal(c, err)
	}
}
