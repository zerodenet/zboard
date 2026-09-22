package entitlementstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSubscriptionMutationsValidateAdminReplayAndAtomicCredentialEffects(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "mutations.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: 1, Email: "admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	owner := model.User{ID: 2, Email: "owner@example.test", Password: "unused", Status: "active"}
	for _, account := range []*model.User{&admin, &owner} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	group := model.NodeGroup{ID: 1, Name: "Test group", Code: "test-group", IsEnabled: true}
	node := model.Node{ID: 1, Name: "Test node", Config: "{}"}
	endpoint := model.ProtocolEndpoint{ID: 1, NodeID: node.ID, Name: "Test endpoint", RuntimeKey: "11111111-1111-1111-1111-111111111111", Protocol: "vless", Address: "example.test", Port: 443, PublicPort: 443, IsActive: true}
	for _, value := range []any{&node, &group, &endpoint, &model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	sub := model.Subscription{ID: 55, UserID: owner.ID, NodeGroupID: group.ID, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(24 * time.Hour), FlowTotal: 1000, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{SubscriptionID: sub.ID, UserID: owner.ID, ProtocolEndpointID: endpoint.ID, NodeID: node.ID, CredentialID: "credential-55", PrincipalKey: "principal-55", Secret: "opaque", Status: "active", ExpiresAt: sub.EndAt}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	service := entitlements.SubscriptionMutations{Repository: SubscriptionMutations{DB: db, Publish: networkstore.EnqueueSubscriptionPublications}}
	extend := entitlements.SubscriptionMutationInput{SubscriptionID: sub.ID, Kind: entitlements.SubscriptionExtend, Days: 5, Reason: "support adjustment", IdempotencyKey: "extend-55", Origin: "plugin:example.operator"}
	if _, err := service.Apply(context.Background(), owner.ID, extend); !errors.Is(err, entitlements.ErrAdministrativeRead) {
		t.Fatalf("owner extended own service: %v", err)
	}
	first, err := service.Apply(context.Background(), admin.ID, extend)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Apply(context.Background(), admin.ID, extend)
	if err != nil || second.MutationID != first.MutationID || second.EndAt != first.EndAt {
		t.Fatalf("replay changed receipt: %+v %+v %v", first, second, err)
	}
	tampered := extend
	tampered.Days = 6
	if _, err := service.Apply(context.Background(), admin.ID, tampered); !errors.Is(err, entitlements.ErrSubscriptionMutationConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	if err := db.First(&sub, sub.ID).Error; err != nil || !sub.EndAt.Equal(first.EndAt) {
		t.Fatalf("term not persisted: %+v %v", sub, err)
	}
	if err := db.First(&credential, credential.ID).Error; err != nil || !credential.ExpiresAt.Equal(first.EndAt) {
		t.Fatalf("credential term diverged: %+v %v", credential, err)
	}
	var count int64
	if err := db.Model(&model.SubscriptionMutation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("mutation count: %d %v", count, err)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ? AND detail LIKE ?", "subscription.term.extend", "%\"origin\":\"plugin:example.operator\"%").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("audit count: %d %v", count, err)
	}
	if err := db.Model(&model.NodeConfigPublish{}).Count(&count).Error; err != nil || count == 0 {
		t.Fatalf("publication missing: %d %v", count, err)
	}
	cancel := entitlements.SubscriptionMutationInput{SubscriptionID: sub.ID, Kind: entitlements.SubscriptionCancel, Reason: "support cancellation", IdempotencyKey: "cancel-55", Origin: "plugin:example.operator"}
	canceled, err := service.Apply(context.Background(), admin.ID, cancel)
	if err != nil || canceled.Status != "canceled" {
		t.Fatalf("cancel: %+v %v", canceled, err)
	}
	if err := db.First(&credential, credential.ID).Error; err != nil || credential.Status != "revoked" || credential.RevokedAt == nil {
		t.Fatalf("credential not revoked: %+v %v", credential, err)
	}
	if err := db.First(&sub, sub.ID).Error; err != nil || sub.Status != "canceled" {
		t.Fatalf("subscription not canceled: %+v %v", sub, err)
	}
	if _, err := service.Apply(context.Background(), admin.ID, extend); err != nil {
		t.Fatalf("prior receipt disappeared after cancel: %v", err)
	}
	if _, err := service.Apply(context.Background(), admin.ID, entitlements.SubscriptionMutationInput{SubscriptionID: sub.ID, Kind: entitlements.SubscriptionExtend, Days: 1, Reason: "new extension", IdempotencyKey: "late-extend"}); !errors.Is(err, entitlements.ErrSubscriptionMutationInvalid) {
		t.Fatalf("canceled subscription extended: %v", err)
	}
	rollback := model.Subscription{ID: 56, UserID: owner.ID, NodeGroupID: group.ID, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(24 * time.Hour), FlowTotal: 1000, Config: "{}"}
	if err := db.Create(&rollback).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("publication unavailable")
	failing := entitlements.SubscriptionMutations{Repository: SubscriptionMutations{DB: db, Publish: func(*gorm.DB, uint, uint) error { return failure }}}
	if _, err := failing.Apply(context.Background(), admin.ID, entitlements.SubscriptionMutationInput{SubscriptionID: rollback.ID, Kind: entitlements.SubscriptionExtend, Days: 1, Reason: "failed publish", IdempotencyKey: "rollback"}); !errors.Is(err, failure) {
		t.Fatalf("publication failure: %v", err)
	}
	var persisted model.Subscription
	if err := db.First(&persisted, rollback.ID).Error; err != nil || !persisted.EndAt.Equal(rollback.EndAt) {
		t.Fatalf("failed publication committed term: %+v %v", persisted, err)
	}
	if err := db.Model(&model.SubscriptionMutation{}).Where("idempotency_key = ?", "rollback").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed publication committed receipt: %d %v", count, err)
	}
}
