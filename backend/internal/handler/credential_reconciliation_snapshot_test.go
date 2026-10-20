package handler

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCredentialReconciliationSQLiteUsesCurrentSubscription(t *testing.T) {
	assertCredentialReconciliationCurrentSubscription(t, newOrderFixture(t))
}
func TestCredentialReconciliationMySQLUsesCurrentSubscription(t *testing.T) {
	assertCredentialReconciliationCurrentSubscription(t, newMySQLOrderFixture(t))
}
func assertCredentialReconciliationCurrentSubscription(t *testing.T, f orderFixture) {
	t.Helper()
	endpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var stale model.Subscription
	if err := f.h.db.First(&stale, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	// A job's previously captured active snapshot must not resurrect actual
	// retirement, including its deliberately destroyed credential material.
	if _, err := f.h.services.CredentialExpiry(f.h.credentialCipher).ExpireDue(context.Background(), stale.EndAt.Add(8*24*time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	var current model.Subscription
	if err := f.h.db.First(&current, stale.ID).Error; err != nil || current.Status != "ended" {
		t.Fatal(current.Status, err)
	}
	var before model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", stale.ID, endpoint.ID).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before.Status != "revoked" || before.Secret != "" || before.RetiredPrincipalHash == "" {
		t.Fatal("retirement did not revoke/destroy canonical credential")
	}
	for range 2 {
		credentials, err := application.EnsureSubscriptionCredentials(f.h.db, stale, f.h.credentialCipher, true)
		if err != nil || len(credentials) != 0 {
			t.Fatal("stale job granted ended subscription", len(credentials), err)
		}
	}
	var after model.ProtocolCredential
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.Secret != before.Secret || after.RetiredPrincipalHash != before.RetiredPrincipalHash || after.RevokedAt == nil || !after.RevokedAt.Equal(*before.RevokedAt) {
		t.Fatal("stale reconciliation changed retired credential")
	}
}

func TestCredentialReconciliationSQLitePreservesRenewedExpiry(t *testing.T) {
	assertCredentialReconciliationRenewedExpiry(t, newOrderFixture(t))
}
func TestCredentialReconciliationMySQLPreservesRenewedExpiry(t *testing.T) {
	assertCredentialReconciliationRenewedExpiry(t, newMySQLOrderFixture(t))
}
func assertCredentialReconciliationRenewedExpiry(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var stale model.Subscription
	if err := f.h.db.First(&stale, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, f.create(t, stale.ID).ID)
	var renewed model.Subscription
	if err := f.h.db.First(&renewed, stale.ID).Error; err != nil || !renewed.EndAt.After(stale.EndAt) {
		t.Fatal("renewal failed", err)
	}
	credentials, err := application.EnsureSubscriptionCredentials(f.h.db, stale, f.h.credentialCipher, true)
	if err != nil || len(credentials) != 1 || !credentials[0].ExpiresAt.Equal(renewed.EndAt) {
		t.Fatal("stale job reverted credential renewal", err)
	}
	// Quota exhaustion is independent of subscription expiry. A stale active
	// snapshot cannot reactivate credentials after reliable metering exhausted it.
	if err := f.h.db.Model(&renewed).Update("flow_used", renewed.FlowTotal).Error; err != nil {
		t.Fatal(err)
	}
	if err := application.ExpireSubscriptionCredentials(f.h.db, renewed.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	credentials, err = application.EnsureSubscriptionCredentials(f.h.db, stale, f.h.credentialCipher, true)
	if err != nil || len(credentials) != 0 {
		t.Fatal("exhausted quota regranted", len(credentials), err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", renewed.ID).First(&credential).Error; err != nil || credential.Status != "expired" {
		t.Fatal(credential.Status, err)
	}
}

func TestCredentialReconciliationSQLitePreservesChangedGroup(t *testing.T) {
	assertCredentialReconciliationChangedGroup(t, newOrderFixture(t))
}
func TestCredentialReconciliationMySQLPreservesChangedGroup(t *testing.T) {
	assertCredentialReconciliationChangedGroup(t, newMySQLOrderFixture(t))
}
func assertCredentialReconciliationChangedGroup(t *testing.T, f orderFixture) {
	t.Helper()
	oldEndpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var stale model.Subscription
	if err := f.h.db.First(&stale, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "new-group", Code: "new-group", IsEnabled: true}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	f.group = group
	newEndpoint := model.ProtocolEndpoint{NodeID: oldEndpoint.NodeID, Name: "new-group-endpoint", RuntimeKey: "00000000-0000-4000-8000-000000000002", Protocol: "vless", Port: 8443, PublicPort: 8443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000}
	if err := f.h.db.Create(&newEndpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: newEndpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	f.planRecord = f.plan(t, 2)
	if err := f.h.db.Model(&f.planRecord).Update("traffic_bytes", 2048).Error; err != nil {
		t.Fatal(err)
	}
	f.skuRecord = f.sku(t, f.planRecord.ID, 200, skuOperationChange)
	order := createLegacyChange(t, f, stale.ID)
	f.paid(t, order.ID)
	credentials, err := application.EnsureSubscriptionCredentials(f.h.db, stale, f.h.credentialCipher, true)
	if err != nil || len(credentials) != 1 || credentials[0].ProtocolEndpointID != newEndpoint.ID {
		t.Fatal("old snapshot reapplied old group", len(credentials), err)
	}
	var old model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", stale.ID, oldEndpoint.ID).First(&old).Error; err != nil || old.Status != "revoked" || old.RevokedAt == nil {
		t.Fatal("old group credential regranted", old.Status, err)
	}
}
