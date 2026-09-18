package plugins

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTransientLeaseRenewalFailureKeepsOwnerAndRetries(t *testing.T) {
	m, db, _ := testManager(t, nil)
	if err := db.Exec(`CREATE TRIGGER fail_renew BEFORE UPDATE ON plugin_host_leases BEGIN SELECT RAISE(ABORT, 'injected outage'); END`).Error; err != nil {
		t.Fatal(err)
	}
	m.renewLease()
	if m.lost.Load() || m.leaseErrors.Load() != 1 {
		t.Fatal("transient error discarded a valid lease")
	}
	db.Exec(`DROP TRIGGER fail_renew`)
	before := m.leaseUntil.Load()
	m.renewLease()
	if m.lost.Load() || m.leaseUntil.Load() <= before {
		t.Fatal("renewal did not resume")
	}
}
func TestStandbyTakesOverExpiredLease(t *testing.T) {
	owner, db, opts := testManager(t, nil)
	standby, err := NewManager(db, owner.cipher, opts, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer standby.Close()
	if !standby.lost.Load() {
		t.Fatal("standby acquired active lease")
	}
	owner.Close()
	standby.superviseOnce(context.Background())
	if standby.lost.Load() {
		t.Fatal("standby never took over")
	}
	var lease model.PluginHostLease
	db.First(&lease, 1)
	if lease.Owner != standby.owner {
		t.Fatal("wrong owner")
	}
}
func TestAuthenticatedHeartbeatRenewsSessionAndDBErrorIsTemporary(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "test", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession(v.ID, "home", "public", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s.ExpiresAt = time.Now().Add(time.Second)
	m.sessions[s.Token] = s
	m.mu.Unlock()
	renewed, err := m.CheckSession(s.Token, 0, false)
	if err != nil || time.Until(renewed.ExpiresAt) < 9*time.Minute {
		t.Fatal("session did not renew", err)
	}
	if err := db.Exec("ALTER TABLE plugin_versions RENAME TO hidden_versions").Error; err != nil {
		t.Fatal(err)
	}
	_, err = m.CheckSession(s.Token, 0, false)
	db.Exec("ALTER TABLE hidden_versions RENAME TO plugin_versions")
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidSession) {
		t.Fatal("temporary DB outage revoked session", err)
	}
	if _, err = m.CheckSession(s.Token, 0, false); err != nil {
		t.Fatal("session could not recover", err)
	}
}
