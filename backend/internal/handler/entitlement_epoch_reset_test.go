package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestClosedEpochLateTrafficDoesNotPolluteCurrentScheduledReset(t *testing.T) {
	assertEpochReset(t, newOrderFixture(t))
}
func TestMySQLClosedEpochLateTrafficDoesNotPolluteCurrentScheduledReset(t *testing.T) {
	assertEpochReset(t, newMySQLOrderFixture(t))
}
func assertEpochReset(t *testing.T, base orderFixture) {
	attachOrderPublishEndpoint(t, base)
	f := newRenewalValueFixture(t, base)
	var old model.ProtocolCredential
	if err := base.h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&old).Error; err != nil {
		t.Fatal(err)
	}
	oldPlain, err := base.h.credentialCipher.Decrypt(old.Secret)
	if err != nil {
		t.Fatal(err)
	}
	*f.at = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	recovery := f.create(t, f.monthly, f.subscription, "reset-epoch-recovery")
	f.pay(t, recovery.ID)
	var fresh model.ProtocolCredential
	if err := base.h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	freshPlain, err := base.h.credentialCipher.Decrypt(fresh.Secret)
	if err != nil {
		t.Fatal(err)
	}
	// Actual repository writes under a trusted simulated clock, matching the
	// purchase fixture's time. No historical paid snapshot is reconstructed.
	*f.at = time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	db := base.h.db.Session(&gorm.Session{NowFunc: func() time.Time { return *f.at }})
	accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: db, Cipher: base.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
	oldEvent := metering.CompletedFlow{NodeID: old.NodeID, CoreInstanceID: "epoch-reset-core", EventID: "old-epoch-reset-completion", EventType: "flow.completed", Sequence: 1, FlowID: "old-epoch-flow", PrincipalKey: oldPlain, BytesDown: 20, OccurredAt: *f.at}
	if record, exhausted, err := accounting.Complete(t.Context(), oldEvent); err != nil || exhausted || record.UsedBytes != 20 || record.EntitlementEpoch != 0 {
		t.Fatalf("late old quota %+v %v %v", record, exhausted, err)
	}
	currentEvent := oldEvent
	currentEvent.EventID = "current-epoch-reset-completion"
	currentEvent.FlowID = "current-epoch-flow"
	currentEvent.PrincipalKey = freshPlain
	currentEvent.Sequence = 2
	currentEvent.BytesDown = 30
	if record, exhausted, err := accounting.Complete(t.Context(), currentEvent); err != nil || exhausted || record.UsedBytes != 30 || record.EntitlementEpoch != 1 {
		t.Fatalf("current quota %+v %v %v", record, exhausted, err)
	}
	*f.at = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	early := f.create(t, f.monthly, f.subscription, "reset-epoch-early-renewal")
	f.pay(t, early.ID)
	var sub model.Subscription
	if err := db.First(&sub, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	if sub.EntitlementEpoch != 1 || total != 100 || used != 30 || sub.CycleStartUsed != 80 || sub.FlowUsed != 110 || sub.NextResetAt == nil || !sub.NextResetAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("old received-after-boundary usage polluted current reset: total=%d used=%d state=%+v", total, used, sub)
	}
}
