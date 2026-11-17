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
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

func TestRegisteredRouteShutdownPreservesForeignLeaseAndCoreIntents(t *testing.T) {
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "route-shutdown.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(db)
	ctx := t.Context()
	handlers := []string{"node_publish_0", "node_publish_1", "traffic_reset", "proxy_pool_sync", "fair_use", "history_retention", "credential_expiry", "event_consumer"}
	submit := func(id, key string) jobs.Run {
		t.Helper()
		run, err := store.Submit(ctx, jobs.Submission{Owner: "system", Key: key, Handler: id, Resource: "foreign-shutdown-resource", Payload: `{"revision":"1"}`})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	active := submit(handlers[0], "foreign-active")
	lease, err := store.Claim(ctx, "surviving-process", handlers, time.Minute)
	if err != nil || lease.Run.ID != active.ID {
		t.Fatal("foreign lease not acquired", lease, err)
	}
	var pending []jobs.Run
	for _, id := range handlers {
		pending = append(pending, submit(id, "foreign-pending-"+id))
	}
	cipher, err := security.NewCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	var rc rest.RestConf
	if err := conf.FillDefault(&rc); err != nil {
		t.Fatal(err)
	}
	rc.Name, rc.Host, rc.Port = "route-shutdown-test", "127.0.0.1", 0
	srv, err := rest.NewServer(rc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	closeRuntime, err := RegisterRoutes(srv, db, "0123456789abcdef0123456789abcdef", cipher, t.TempDir(), "legacy", "", zeroevent.Config{Enabled: false}, plugins.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeRuntime() })
	if err := closeRuntime(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(context.Background(), "system", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]jobs.State{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	if states[active.ID] != jobs.Running {
		t.Fatalf("route cleanup revoked a foreign lease: %s", states[active.ID])
	}
	for _, run := range pending {
		if states[run.ID] != jobs.Queued {
			t.Fatalf("route cleanup revoked %s intent: %s", run.Handler, states[run.ID])
		}
	}
	if err := store.Finish(ctx, lease, jobs.Succeeded); err != nil {
		t.Fatal("surviving owner lost completion fence", err)
	}
}
