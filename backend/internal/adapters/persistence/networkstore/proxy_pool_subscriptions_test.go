package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProxyPoolSubscriptionsCommitClaimFailureAndAuditAtomically(t *testing.T) {
	db, competing := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	pool := model.NodeProxyPool{NodeID: 1, Name: "subscription", Config: "enc:old", SubscriptionURL: "enc:https://example.test/sub", SubscriptionFormat: "zero", AutoSync: true, SyncIntervalSeconds: 600, NextSyncAt: &due, Revision: 1}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	store := ProxyPoolSubscriptions{DB: db}
	claims, err := store.ClaimDueProxyPoolSubscriptions(context.Background(), due, due.Add(5*time.Minute), 10)
	if err != nil || len(claims) != 1 || claims[0].ID != pool.ID || claims[0].Revision != 1 {
		t.Fatalf("claims=%+v error=%v", claims, err)
	}
	otherClaims, err := (ProxyPoolSubscriptions{DB: competing}).ClaimDueProxyPoolSubscriptions(context.Background(), due, due.Add(5*time.Minute), 10)
	if err != nil || len(otherClaims) != 0 {
		t.Fatalf("competing claims=%+v error=%v", otherClaims, err)
	}
	actor := network.ProxyPoolSubscriptionActor{System: true}
	snapshot, err := store.LoadProxyPoolSubscription(context.Background(), actor, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := due.Add(time.Minute)
	updated, err := store.CommitProxyPoolSubscription(context.Background(), actor, snapshot, "enc:new", 3, network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, now)
	if err != nil || updated.Revision != 2 || updated.SubscriptionNodeCount != 3 || updated.LastSyncAt == nil || !updated.LastSyncAt.Equal(now) {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	var audit model.AuditLog
	if err := db.Where("action = ?", "node_proxy_pool.subscription.sync").First(&audit).Error; err != nil || audit.Actor != "system:proxy-pool-subscription" {
		t.Fatalf("audit=%+v error=%v", audit, err)
	}
	var publish model.NodeConfigPublish
	if err := db.First(&publish, "node_id = ?", 1).Error; err != nil || publish.RequestedBy != 0 {
		t.Fatalf("publish=%+v error=%v", publish, err)
	}
	if err := store.RecordProxyPoolSubscriptionFailure(context.Background(), actor, snapshot, "stale", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var current model.NodeProxyPool
	if err := db.First(&current, pool.ID).Error; err != nil || current.LastSyncError != "" {
		t.Fatalf("stale failure overwrote result: %+v error=%v", current, err)
	}
	fresh := proxyPoolSubscriptionSnapshot(current)
	if err := store.RecordProxyPoolSubscriptionFailure(context.Background(), actor, fresh, "network failed", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&current, pool.ID).Error; err != nil || current.LastSyncError != "network failed" {
		t.Fatalf("failure missing: %+v error=%v", current, err)
	}
}

func TestProxyPoolSubscriptionAuditFailureAndRevokedAdminRollBack(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	pool := model.NodeProxyPool{NodeID: 1, Name: "subscription", Config: "enc:old", SubscriptionURL: "enc:url", SyncIntervalSeconds: 600, Revision: 1}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	store := ProxyPoolSubscriptions{DB: db}
	actor := network.ProxyPoolSubscriptionActor{AccountID: 1}
	snapshot, err := store.LoadProxyPoolSubscription(context.Background(), actor, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitProxyPoolSubscription(context.Background(), actor, snapshot, "enc:denied", 1, network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, time.Now().UTC()); !errors.Is(err, network.ErrProxyPoolMutationPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
	if err := store.RecordProxyPoolSubscriptionFailure(context.Background(), actor, snapshot, "denied", time.Now().UTC()); !errors.Is(err, network.ErrProxyPoolMutationPermission) {
		t.Fatalf("revoked failure write error=%v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_proxy_pool_sync_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("fail_proxy_pool_sync_audit") })
	if _, err := store.CommitProxyPoolSubscription(context.Background(), actor, snapshot, "enc:rollback", 1, network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, time.Now().UTC()); err == nil {
		t.Fatal("audit failure accepted")
	}
	var current model.NodeProxyPool
	if err := db.First(&current, pool.ID).Error; err != nil || current.Config != "enc:old" || current.Revision != 1 {
		t.Fatalf("sync escaped rollback: %+v error=%v", current, err)
	}
}
