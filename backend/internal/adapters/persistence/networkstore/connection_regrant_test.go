package networkstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestConnectionRevocationMySQLAndSQLiteFencesGroupRegrant(t *testing.T) {
	db, other := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "active", FlowTotal: 100, Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)},
		&model.ProtocolCredential{ID: 1, SubscriptionID: 1, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: "regrant", PrincipalKey: "regrant", Status: "revoked", ExpiresAt: now.Add(time.Hour), RevokedAt: &now},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	authority := ConnectionRevocation{DB: db}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	finished := make(chan error, 1)
	go func() {
		forbidden, err := authority.WithRevokedPrincipal(context.Background(), 1, "regrant", func() error { close(entered); <-release; return nil })
		if err == nil && !forbidden {
			err = errors.New("removed membership was allowed")
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("native command did not enter")
	}
	regrant := make(chan error, 1)
	go func() {
		revision := uint64(1)
		endpoints := []uint{1}
		_, err := nodeGroupMutationService(other).Update(context.Background(), 1, network.NodeGroupUpdateRequest{ID: 1, ExpectedRevision: &revision, ProtocolEndpointIDs: &endpoints, CredentialProtocols: []string{"vless"}})
		regrant <- err
	}()
	select {
	case err := <-regrant:
		t.Fatal("membership committed during native command", err)
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-regrant; err != nil {
		t.Fatal(err)
	}
	// Real membership mutation has committed, but its async credential job has
	// not run. The old revoked projection cannot justify closing the regrant.
	called := false
	forbidden, err := authority.WithRevokedPrincipal(context.Background(), 1, "regrant", func() error { called = true; return nil })
	if !errors.Is(err, network.ErrConnectionRevocationPending) || forbidden || called {
		t.Fatal("regranted stale projection caused closure", forbidden, called, err)
	}
	var jobs int64
	if err := db.Model(&model.Task{}).Count(&jobs).Error; err != nil || jobs == 0 {
		t.Fatal("reconciliation intent missing", jobs, err)
	}
	// Once its projection is active, this identity is explicitly allowed.
	if err := db.Model(&model.ProtocolCredential{}).Where("id = ?", 1).Updates(map[string]any{"status": "active", "revoked_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	forbidden, err = authority.WithRevokedPrincipal(context.Background(), 1, "regrant", func() error { t.Error("authorized regrant closed"); return nil })
	if err != nil || forbidden {
		t.Fatal(forbidden, err)
	}
	// Membership remains the authority even if a revocation projection lags.
	if err := db.Where("node_group_id = ?", 1).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	called = false
	forbidden, err = authority.WithRevokedPrincipal(context.Background(), 1, "regrant", func() error { called = true; return nil })
	if err != nil || !forbidden || !called {
		t.Fatal("removed membership accepted stale active projection", forbidden, called, err)
	}
}
