package entitlementstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type credentialIssuerStub struct{ secrets int }

func (s *credentialIssuerStub) Supports(protocol string) bool { return protocol == "vless" }
func (s *credentialIssuerStub) Status(string, bool) string    { return "active" }
func (s *credentialIssuerStub) Secret(string, string) (string, error) {
	s.secrets++
	return "issued-secret", nil
}
func (s *credentialIssuerStub) Encrypt(secret string) (string, error) {
	return "encrypted:" + secret, nil
}

func credentialReconciliationFixture(t *testing.T) (*GroupCredentialReconciliation, *credentialIssuerStub, model.NodeGroup, model.ProtocolEndpoint, model.Subscription) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: 1, Email: "credential-owner@example.test", Password: "hash", Status: "active"}
	node := model.Node{ID: 1, Name: "credential-node"}
	group := model.NodeGroup{ID: 1, Name: "credential-group", Code: "credential-group", IsEnabled: true, Revision: 1}
	endpoint := model.ProtocolEndpoint{ID: 1, NodeID: node.ID, Name: "vless", RuntimeKey: "credential-endpoint", Protocol: "vless", Address: "node.example.test", Port: 443, PublicPort: 8443, ServerConfig: "protected", IsActive: true}
	subscription := model.Subscription{ID: 1, UserID: user.ID, NodeGroupID: group.ID, Status: "active", EndAt: time.Now().UTC().Add(time.Hour), FlowTotal: 1024}
	for _, record := range []interface{}{&user, &node, &group, &endpoint, &model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}, &subscription} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	issuer := &credentialIssuerStub{}
	return &GroupCredentialReconciliation{DB: db, Issuer: issuer}, issuer, group, endpoint, subscription
}

func TestGroupCredentialReconciliationIsIdempotentAndRevokesRemovedMembership(t *testing.T) {
	store, issuer, group, endpoint, subscription := credentialReconciliationFixture(t)
	now := time.Now().UTC()
	if err := store.ReconcileGroup(context.Background(), group.ID, now); err != nil {
		t.Fatal(err)
	}
	var credential model.ProtocolCredential
	if err := store.DB.Where("subscription_id = ? AND protocol_endpoint_id = ?", subscription.ID, endpoint.ID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != "active" || credential.Secret != "encrypted:issued-secret" || issuer.secrets != 1 {
		t.Fatalf("credential not issued: credential=%+v secrets=%d", credential, issuer.secrets)
	}
	if err := store.ReconcileGroup(context.Background(), group.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if issuer.secrets != 1 {
		t.Fatalf("current credential was reissued: secrets=%d", issuer.secrets)
	}
	if err := store.DB.Where("node_group_id = ? AND protocol_endpoint_id = ?", group.ID, endpoint.ID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	revokedAt := now.Add(2 * time.Second)
	if err := store.ReconcileGroup(context.Background(), group.ID, revokedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&credential, credential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != "revoked" || credential.RevokedAt == nil || !credential.RevokedAt.Equal(revokedAt) {
		t.Fatalf("removed membership remained active: %+v", credential)
	}
	if err := store.DB.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileGroup(context.Background(), group.ID, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	credentialID := credential.ID
	credential = model.ProtocolCredential{}
	if err := store.DB.First(&credential, credentialID).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != "active" || credential.RevokedAt != nil || issuer.secrets != 1 {
		t.Fatalf("existing credential was not reactivated: credential=%+v secrets=%d", credential, issuer.secrets)
	}
}

func TestGroupCredentialReconciliationHonorsCancellation(t *testing.T) {
	store, _, group, _, _ := credentialReconciliationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.ReconcileGroup(ctx, group.ID, time.Now().UTC()); err == nil {
		t.Fatal("canceled reconciliation succeeded")
	}
}

func TestNodeCredentialReconciliationSelectsConnectedGroups(t *testing.T) {
	groupStore, issuer, _, endpoint, subscription := credentialReconciliationFixture(t)
	store := NodeCredentialReconciliation{DB: groupStore.DB, Issuer: issuer}
	if err := store.ReconcileNode(context.Background(), endpoint.NodeID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var credential model.ProtocolCredential
	if err := store.DB.Where("subscription_id = ? AND protocol_endpoint_id = ?", subscription.ID, endpoint.ID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != "active" || issuer.secrets != 1 {
		t.Fatalf("connected group was not reconciled: credential=%+v secrets=%d", credential, issuer.secrets)
	}
	if err := store.ReconcileNode(context.Background(), 999, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if issuer.secrets != 1 {
		t.Fatalf("unconnected node reconciled credentials: secrets=%d", issuer.secrets)
	}
}

func TestNodeCredentialReconciliationHonorsCancellation(t *testing.T) {
	groupStore, issuer, _, endpoint, _ := credentialReconciliationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (NodeCredentialReconciliation{DB: groupStore.DB, Issuer: issuer}).ReconcileNode(ctx, endpoint.NodeID, time.Now().UTC())
	if err == nil {
		t.Fatal("canceled node reconciliation succeeded")
	}
}

func TestCredentialPreparationEnsuresExplicitTargetsAndListsActiveMieru(t *testing.T) {
	groupStore, issuer, group, endpoint, subscription := credentialReconciliationFixture(t)
	store := CredentialPreparation{DB: groupStore.DB, Issuer: issuer}
	if err := store.EnsureCredentialSubscriptions(context.Background(), []entitlements.CredentialSubscription{{ID: subscription.ID, UserID: subscription.UserID, NodeGroupID: group.ID, EndAt: subscription.EndAt}}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := store.DB.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", subscription.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("credential count = %d err=%v", count, err)
	}
	if err := store.DB.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoint.ID).Update("protocol", "mieru").Error; err != nil {
		t.Fatal(err)
	}
	targets, err := store.ListActiveMieruCredentialSubscriptions(context.Background(), time.Now().UTC())
	if err != nil || len(targets) != 1 || targets[0].ID != subscription.ID {
		t.Fatalf("active Mieru targets = %+v err=%v", targets, err)
	}
}
