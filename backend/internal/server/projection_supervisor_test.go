//go:build linux

package server

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

type forbiddenCapacityStart struct{}

func (forbiddenCapacityStart) Start(context.Context) (localworkers.Process, error) {
	panic("busy ownership launched a worker")
}
func (forbiddenCapacityStart) Sample(time.Time) localworkers.Resources {
	panic("busy ownership started a sampler")
}

func TestProjectionSupervisorBusyOwnerDoesNotBanSingleProcessCore(t *testing.T) {
	dir := t.TempDir()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(dir, "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(db)
	run, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "foreign-capacity-owner", Handler: "foreign-capacity-owner", Resource: "foreign-capacity-owner", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(t.Context(), "foreign-process", []string{run.Handler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "projection.lock")
	owner, err := localworkers.AcquireLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	cipher, err := security.NewCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	var configuration rest.RestConf
	if err := conf.FillDefault(&configuration); err != nil {
		t.Fatal(err)
	}
	configuration.Name, configuration.Host, configuration.Port = "busy-supervisor-test", "127.0.0.1", 0
	srv, err := rest.NewServer(configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	policy := localworkers.DefaultPolicy()
	policy.MaxWorkers = 1
	options := &application.ProjectionSupervisorOptions{Policy: policy, Interval: time.Second, DrainTimeout: time.Second, LockPath: lock, Launcher: forbiddenCapacityStart{}, Resources: forbiddenCapacityStart{}}
	closeCore, err := RegisterRoutesWithProjectionSupervisor(srv, db, "0123456789abcdef0123456789abcdef", cipher, t.TempDir(), "legacy", "", zeroevent.Config{Enabled: false}, plugins.Options{Directory: t.TempDir()}, options)
	if err != nil || closeCore == nil {
		t.Fatal("optional capacity ownership banned the complete core", err)
	}
	if err := closeCore(); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(t.Context(), lease, jobs.Succeeded); err != nil {
		t.Fatal("capacity failure revoked a foreign execution", err)
	}
}
