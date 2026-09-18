package entitlementstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestNetworkEntryProjectionUsesFixedBatchAndRequiresLandingGrant(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "network-entry-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nodes := []model.Node{
		{Name: "front", Region: "front-region", Config: "{}", IsEnabled: true, LastSeenAt: &now},
		{Name: "landing", Region: "landing-region", Config: "{}", IsEnabled: true, LastSeenAt: &now},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: nodes[1].ID, Name: "landing", RuntimeKey: "entry-projection-endpoint", Protocol: "vless", Address: "landing.example", Port: 443, PublicPort: 443, ServerConfig: "cipher", ClientConfig: `{}`, OptionalConfig: `{}`, Tags: `[]`, IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "Projection", Code: "entry-projection", IsEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{Name: "front", Network: "tcp_udp", NodeID: nodes[0].ID, EndpointID: endpoint.ID, Address: "front.example", Port: 8443, PublicPort: 8443, Enabled: true, Revision: 1}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{SubscriptionID: 77, UserID: 9, ProtocolEndpointID: endpoint.ID, NodeID: endpoint.NodeID, CredentialID: "entry-credential", PrincipalKey: "entry-principal", Secret: "ciphertext", Status: "active", ExpiresAt: now.Add(time.Hour)}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	store := NetworkEntryProjection{DB: db}
	withoutGrant, err := store.LoadNetworkEntryProjection(context.Background(), []entitlements.NetworkEntryProjectionSubscription{{ID: credential.SubscriptionID, NodeGroupID: group.ID}}, now)
	if err != nil || len(withoutGrant.Entries) != 0 {
		t.Fatalf("projection without landing grant = %+v err=%v", withoutGrant, err)
	}
	if err := db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	counter := &subscriptionProjectionQueryCounter{Interface: db.Logger}
	projection, err := (NetworkEntryProjection{DB: db.Session(&gorm.Session{Logger: counter})}).LoadNetworkEntryProjection(context.Background(), []entitlements.NetworkEntryProjectionSubscription{{ID: credential.SubscriptionID, NodeGroupID: group.ID}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 5 {
		t.Fatalf("network entry projection query count = %d, want 5 fixed queries", got)
	}
	if len(projection.Entries) != 1 || len(projection.Endpoints) != 1 || len(projection.Nodes) != 2 || len(projection.Credentials) != 1 {
		t.Fatalf("projection = %+v", projection)
	}
}
