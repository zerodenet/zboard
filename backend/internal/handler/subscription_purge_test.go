package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func requireSubscriptionDeleted(t *testing.T, db *gorm.DB, id uint) model.ClosedEntitlement {
	t.Helper()
	var sub model.Subscription
	if e := db.Unscoped().First(&sub, id).Error; !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatal("live subscription was not physically deleted", id, e)
	}
	var closed model.ClosedEntitlement
	if e := db.Where("subscription_id=?", id).Order("epoch DESC").Take(&closed).Error; e != nil {
		t.Fatal("minimal late-accounting evidence missing", e)
	}
	return closed
}
func TestSubscriptionWindowDeletionKeepsLateAccounting(t *testing.T) {
	assertSubscriptionDeletionLateAccounting(t, newOrderFixture(t))
}
func TestMySQLSubscriptionWindowDeletionKeepsLateAccounting(t *testing.T) {
	assertSubscriptionDeletionLateAccounting(t, newMySQLBusinessOrderFixture(t))
}
func assertSubscriptionDeletionLateAccounting(t *testing.T, f orderFixture) {
	t.Helper()
	h, c := accountingFixtureFromOrder(t, f)
	if e := h.projectZeroNodeEvents(t.Context(), accountingBenchmarkEvents(c, 0, 1)); e != nil {
		t.Fatal(e)
	}
	var original model.Subscription
	if e := h.db.First(&original, c.SubscriptionID).Error; e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	ended := now.Add(-8 * 24 * time.Hour)
	if e := h.db.Model(&model.Subscription{}).Where("id=?", c.SubscriptionID).Updates(map[string]any{"end_at": ended}).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := h.services.CredentialExpiry(h.credentialCipher).ExpireDue(t.Context(), now, 20); e != nil {
		t.Fatal(e)
	}
	closed := requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
	if closed.FlowUsed != 30 {
		t.Fatal("closing charge changed", closed.FlowUsed)
	}
	for _, table := range []string{"subscription_tokens", "subscription_members", "subscription_automatic_renewals", "quota_events"} {
		var n int64
		if e := h.db.Table(table).Where("subscription_id=?", c.SubscriptionID).Count(&n).Error; e != nil || n != 0 {
			t.Fatal("runtime child retained", table, n, e)
		}
	}
	var credential model.ProtocolCredential
	if e := h.db.First(&credential, c.ID).Error; e != nil || credential.Secret != "" || credential.CurrentScope != nil || credential.Status != "revoked" {
		t.Fatal("usable credential survived", e)
	}
	late := accountingBenchmarkEvents(c, 1, 1)
	for range 2 {
		if e := h.projectZeroNodeEvents(t.Context(), late); e != nil {
			t.Fatal("deleted service rejected historical report", e)
		}
	}
	closed = requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
	if closed.FlowUsed != 60 {
		t.Fatal("late report lost or charged twice", closed.FlowUsed)
	}
	var sums struct{ Raw, Used int64 }
	if e := h.db.Model(&model.TrafficRecord{}).Where("subscription_id=?", c.SubscriptionID).Select("COALESCE(SUM(raw_bytes),0) AS raw,COALESCE(SUM(used_bytes),0) AS used").Scan(&sums).Error; e != nil || sums.Raw != 60 || sums.Used != 60 {
		t.Fatal("ledger lost/doubled", sums, e)
	}
	var subject model.AccountSubject
	if e := h.db.First(&subject, c.UserID).Error; e != nil || subject.LastEntitlementEnd == nil || !subject.LastEntitlementEnd.Equal(ended.Truncate(time.Millisecond)) { // SQLite retains nanoseconds, MySQL keeps milliseconds.
		if e != nil || subject.LastEntitlementEnd == nil || subject.LastEntitlementEnd.Sub(ended) > time.Millisecond || ended.Sub(*subject.LastEntitlementEnd) > time.Millisecond {
			t.Fatal("account inactivity anchor lost", subject, e)
		}
	}
	completed := metering.CompletedFlow{NodeID: c.NodeID, CoreInstanceID: "benchmark-core", EventID: "deleted-completed", FlowID: "flow-0", PrincipalKey: c.PrincipalKey, EventType: "flow.completed", Sequence: 3, Revision: 3, BytesUp: 30, BytesDown: 60, OccurredAt: now.Add(time.Second)}
	completion := meteringstore.CompletionAccounting{DB: h.db, Cipher: h.credentialCipher}
	for range 2 {
		if _, exhausted, e := completion.Complete(t.Context(), completed); e != nil || exhausted {
			t.Fatal("deleted completion lost/doubled or revived service", e)
		}
	}
	closed = requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
	if closed.FlowUsed != 90 {
		t.Fatal("completion charged incorrectly", closed.FlowUsed)
	}
	// Deleted services must still participate in existing summary retention:
	// compaction cannot strand detail rows or lose replay/charge evidence.
	drainUsageProjections(t, h.db)
	retention := meteringstore.LedgerRetention{DB: h.db, Now: func() time.Time { return now.AddDate(2, 0, 0) }}
	for range 4 {
		if _, e := retention.Drain(t.Context(), 200); e != nil {
			t.Fatal("deleted subscription blocked retention", e)
		}
	}
	if e := h.projectZeroNodeEvents(t.Context(), late); e != nil {
		t.Fatal("compacted old update rejected", e)
	}
	if _, _, e := completion.Complete(t.Context(), completed); e != nil {
		t.Fatal("compacted completion replay rejected", e)
	}
	closed = requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
	if closed.FlowUsed != 90 {
		t.Fatal("compacted replay charged again", closed.FlowUsed)
	}
	// Removing the final runtime row must not allow its numeric ID to be reused.
	original.ID = 0
	original.Status = "active"
	original.EndAt = now.Add(24 * time.Hour)
	original.EndedAt = nil
	if e := h.db.Create(&original).Error; e != nil || original.ID == c.SubscriptionID {
		t.Fatal("removed identity reused", original.ID, e)
	}
}
func TestSubscriptionDeletionKeepsAccountCleanupAnchor(t *testing.T) {
	assertSubscriptionCleanupAnchor(t, newOrderFixture(t))
}
func TestMySQLSubscriptionDeletionKeepsAccountCleanupAnchor(t *testing.T) {
	assertSubscriptionCleanupAnchor(t, newMySQLBusinessOrderFixture(t))
}
func assertSubscriptionCleanupAnchor(t *testing.T, f orderFixture) {
	t.Helper()
	h, c := accountingFixtureFromOrder(t, f)
	now := time.Now().UTC()
	ended := now.Add(-8 * 24 * time.Hour)
	if e := h.db.Model(&model.User{}).Where("id=?", c.UserID).Updates(map[string]any{"is_admin": false, "created_at": now.Add(-40 * 24 * time.Hour)}).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&model.Subscription{}).Where("id=?", c.SubscriptionID).UpdateColumn("end_at", ended).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := h.services.CredentialExpiry(h.credentialCipher).ExpireDue(t.Context(), now, 20); e != nil {
		t.Fatal(e)
	}
	requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
	if e := h.db.Where("config_key=?", identity.CleanupDaysKey).Assign(model.SystemConfig{Value: "14", ValueType: "int"}).FirstOrCreate(&model.SystemConfig{ConfigKey: identity.CleanupDaysKey}).Error; e != nil {
		t.Fatal(e)
	}
	cleaner := identitystore.Deletion{DB: h.db}
	for range 2 {
		if n, e := cleaner.Cleanup(t.Context(), now, 100); e != nil || n != 0 {
			t.Fatal("deleting subscription deleted account early", n, e)
		}
	}
	var user model.User
	if e := h.db.First(&user, c.UserID).Error; e != nil {
		t.Fatal(e)
	}
	deleted := 0
	for range 3 {
		n, e := cleaner.Cleanup(t.Context(), now.Add(7*24*time.Hour), 100)
		if e != nil {
			t.Fatal(e)
		}
		deleted += n
	}
	if deleted != 1 {
		t.Fatal("inactivity cleanup did not converge from original end time", deleted)
	}
}

func TestSubscriptionCanceledAndLegacyEndedAreDeleted(t *testing.T) {
	for _, status := range []string{"canceled", "ended"} {
		t.Run(status, func(t *testing.T) {
			f := newOrderFixture(t)
			h, c := accountingFixtureFromOrder(t, f)
			now := time.Now().UTC()
			ended := now.Add(-time.Hour)
			if e := h.db.Model(&model.Subscription{}).Where("id=?", c.SubscriptionID).Updates(map[string]any{"status": status, "ended_at": ended, "end_reason": "canceled"}).Error; e != nil {
				t.Fatal(e)
			}
			if _, e := h.services.CredentialExpiry(h.credentialCipher).ExpireDue(t.Context(), now, 20); e != nil {
				t.Fatal(e)
			}
			requireSubscriptionDeleted(t, h.db, c.SubscriptionID)
			if due, e := h.services.CredentialExpiry(h.credentialCipher).HasDue(t.Context(), now); e != nil || due {
				t.Fatal("terminal cleanup did not converge", due, e)
			}
		})
	}
}
