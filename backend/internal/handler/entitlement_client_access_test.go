package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestClientAccessBindsTokenAndFencesUsageAfterRotation(t *testing.T) {
	f := newOrderFixture(t)
	first := f.paid(t, f.create(t, 0).ID)
	second := f.paid(t, f.create(t, 0).ID)
	ctx := context.Background()
	account := f.h.services.SubscriptionAccess(f.h.credentialCipher)
	client := f.h.services.ClientSubscriptionAccess()
	token, err := account.Read(ctx, 1, first.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := client.Resolve(ctx, token.Token)
	if err != nil || grant.Subscription.ID != first.SubscriptionID || grant.Subscription.ID == second.SubscriptionID {
		t.Fatalf("scope: %v", err)
	}
	replacement, err := account.Rotate(ctx, 1, first.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(ctx, token.Token); !errors.Is(err, entitlements.ErrAccessPermission) {
		t.Fatalf("old token: %v", err)
	}
	if err := client.MarkUsed(ctx, token.Token, grant.AccessID, time.Now()); err != nil {
		t.Fatal(err)
	}
	var stored model.SubscriptionToken
	if err := f.h.db.First(&stored, grant.AccessID).Error; err != nil || stored.LastUsedAt != nil {
		t.Fatal("old render marked replacement used")
	}
	if _, err := client.Resolve(ctx, replacement.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := account.Revoke(ctx, 1, first.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(ctx, replacement.Token); !errors.Is(err, entitlements.ErrAccessPermission) {
		t.Fatalf("revoked token: %v", err)
	}
	secondToken, err := account.Read(ctx, 1, second.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", second.SubscriptionID).Update("flow_used", 1024).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(ctx, secondToken.Token); !errors.Is(err, entitlements.ErrAccessInactive) {
		t.Fatalf("exhausted: %v", err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, second.SubscriptionID).Error; err != nil || sub.Status != "expired" {
		t.Fatal("expiry was not committed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := client.Resolve(canceled, secondToken.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
