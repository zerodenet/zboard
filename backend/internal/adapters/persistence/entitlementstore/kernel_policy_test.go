package entitlementstore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestKernelPolicyRevisionIgnoresTrafficButAdvancesForPolicyChanges(t *testing.T) {
	store, _, group, _, sub := credentialReconciliationFixture(t)
	reconcile := func() model.Subscription {
		t.Helper()
		if err := store.ReconcileGroup(context.Background(), group.ID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		var current model.Subscription
		if err := store.DB.First(&current, sub.ID).Error; err != nil {
			t.Fatal(err)
		}
		return current
	}
	first := reconcile()
	if first.KernelPolicyRevision == 0 {
		t.Fatal("policy was not prepared")
	}
	if err := store.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"flow_used": 16, "updated_at": time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	traffic := reconcile()
	if traffic.KernelPolicyRevision != first.KernelPolicyRevision || traffic.KernelPolicyFingerprint != first.KernelPolicyFingerprint {
		t.Fatal("traffic changed native policy")
	}
	if err := store.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("speed_limit_mbps", 25).Error; err != nil {
		t.Fatal(err)
	}
	changed := reconcile()
	if changed.KernelPolicyRevision != first.KernelPolicyRevision+1 {
		t.Fatal("speed policy revision did not advance")
	}
	if err := store.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("speed_limit_mbps", first.SpeedLimitMbps).Error; err != nil {
		t.Fatal(err)
	}
	if restored := reconcile(); restored.KernelPolicyRevision != changed.KernelPolicyRevision+1 {
		t.Fatal("A to B to A reused a policy revision")
	}
}
