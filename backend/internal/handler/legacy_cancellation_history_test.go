package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestLegacyCancellationPreservesHistoricalReceipt(t *testing.T) {
	assertLegacyCancelHistory(t, newOrderFixture(t))
}
func TestMySQLLegacyCancellationPreservesHistoricalReceipt(t *testing.T) {
	assertLegacyCancelHistory(t, newMySQLBusinessOrderFixture(t))
}
func assertLegacyCancelHistory(t *testing.T, f orderFixture) {
	t.Helper()
	sub := model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "canceled", FlowTotal: 100, StartAt: time.Now().UTC(), EndAt: time.Now().UTC().Add(time.Hour), Config: "{}"}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	in := entitlements.SubscriptionMutationInput{SubscriptionID: sub.ID, Kind: entitlements.SubscriptionCancel, Reason: "historical operational cancellation", IdempotencyKey: "old-status-only-receipt", Origin: "plugin:legacy.cancel"}
	row := model.SubscriptionMutation{SubscriptionID: sub.ID, ActorID: 1, Kind: in.Kind, Reason: in.Reason, Origin: in.Origin, IdempotencyKey: in.IdempotencyKey, RequestHash: entitlements.SubscriptionMutationFingerprint(in), Status: "canceled", EndAt: sub.EndAt, CreatedAt: time.Now().UTC()}
	if err := f.h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// Legacy receipts are history, not authorization to run a new termination.
	if err := f.h.db.Model(&sub).Updates(map[string]any{"status": "expired", "end_at": time.Now().UTC().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	var persisted model.SubscriptionMutation
	if err := f.h.db.First(&persisted, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	r, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, in)
	if err != nil || r.MutationID != row.ID || r.Status != "canceled" || !r.EndAt.Equal(persisted.EndAt) {
		t.Fatalf("historical receipt %+v %v", r, err)
	}
	if err := f.h.db.First(&sub, sub.ID).Error; err != nil || sub.Status != "expired" || sub.EndedAt != nil {
		t.Fatalf("historical replay changed rights %+v %v", sub, err)
	}
	for _, table := range []string{"quota_events", "subscription_value_settlements", "wallet_entries", "audit_logs"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("historical replay performed %s %d %v", table, n, err)
		}
	}
	changed := in
	changed.Reason = "a different cancellation"
	if _, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, changed); !errors.Is(err, entitlements.ErrSubscriptionMutationConflict) {
		t.Fatalf("changed historical command %v", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, in); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("historical replay bypassed current authority %v", err)
	}
}
