package networkstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type retirementCompletion struct {
	state jobs.State
	err   error
}

type retirementRuntimeStore struct {
	*jobstore.Store
	finished chan retirementCompletion
}

func (s retirementRuntimeStore) Finish(ctx context.Context, claim jobs.Claim, state jobs.State) error {
	err := s.Store.Finish(ctx, claim, state)
	s.finished <- retirementCompletion{state: state, err: err}
	return err
}

// Executor-only coverage skips Runtime's payload revision fence. Exercise the
// same admission boundary used by the production native job registration.
func TestRetirementRunsThroughVersionedRuntime(t *testing.T) {
	for _, mode := range []string{"detach", "retire"} {
		t.Run(mode, func(t *testing.T) { retirementThroughRuntime(t, mode) })
	}
}

func TestMySQLRetirementRunsThroughVersionedRuntime(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("requires disposable MySQL")
	}
	retirementThroughRuntime(t, "detach")
}

func retirementThroughRuntime(t *testing.T, mode string) {
	t.Helper()
	db, store, remote, service := retirementFixture(t)
	if mode == "detach" {
		if err := db.Model(&model.Node{}).Where("id = ?", 1).Updates(map[string]any{"ssh_host": "", "ssh_host_key_fingerprint": ""}).Error; err != nil {
			t.Fatal(err)
		}
	}
	item, err := store.Start(t.Context(), 1, 1, mode)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan retirementCompletion, 1)
	runtimeErrors := make(chan error, 1)
	runtime := jobs.NewRuntime(retirementRuntimeStore{Store: jobstore.New(db), finished: completed}, "retirement-runtime-test", nil, func(err error) {
		select {
		case runtimeErrors <- err:
		default:
		}
	})
	t.Cleanup(runtime.Close)
	if err := runtime.Register(jobs.Definition{ID: network.NodeRetirementHandler, Handler: network.NodeRetirementHandler, Owner: "system", Revision: network.NodeRetirementRevision, Timeout: time.Minute}, service.Execute); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-completed:
		if result.err != nil || result.state != jobs.Succeeded {
			t.Fatalf("runtime retirement: state=%s err=%v", result.state, result.err)
		}
	case err := <-runtimeErrors:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("retirement did not finish through runtime")
	}
	done, err := store.Read(t.Context(), 1)
	if err != nil || done.State != "succeeded" || done.RunID != item.RunID {
		t.Fatal("missing durable completion", done, err)
	}
	var remaining int64
	if err := db.Model(&model.Node{}).Where("id = ? AND lifecycle_status = ? AND archived_at IS NOT NULL", 1, "archived").Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatal("archive authority missing after successful retirement", remaining, err)
	}
	if mode == "detach" && len(remote.calls) != 0 {
		t.Fatal("offline detach called remote", remote.calls)
	}
}
