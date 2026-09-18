package entitlementstore

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type subscriptionProjectionQueryCounter struct {
	logger.Interface
	count atomic.Int64
}

func (c *subscriptionProjectionQueryCounter) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	c.count.Add(1)
	c.Interface.Trace(ctx, begin, query, err)
}

func TestSubscriptionProjectionLoadsBoundedBatch(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "subscription-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	node := model.Node{Name: "projection-node", Region: "us", Address: "edge.example", Config: "{}", IsEnabled: true, LastSeenAt: &now}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "projection-endpoint", RuntimeKey: "projection-endpoint", Protocol: "vless", Address: node.Address, Port: 443, PublicPort: 443, ServerConfig: "cipher", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "Projection", Code: "projection", IsEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{SubscriptionID: 41, UserID: 9, ProtocolEndpointID: endpoint.ID, NodeID: node.ID, CredentialID: "projection-credential", PrincipalKey: "projection-principal", Secret: "ciphertext", Status: "active", ExpiresAt: now.Add(time.Hour)}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	counter := &subscriptionProjectionQueryCounter{Interface: db.Logger}
	queryDB := db.Session(&gorm.Session{Logger: counter})
	data, err := (SubscriptionProjection{DB: queryDB}).ProjectionData(context.Background(), []uint{credential.SubscriptionID}, []uint{group.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 4 {
		t.Fatalf("projection query count = %d, want 4 fixed batch queries", got)
	}
	if len(data.Credentials) != 1 || len(data.Endpoints) != 1 || len(data.Nodes) != 1 || len(data.Memberships[group.ID]) != 1 {
		t.Fatalf("projection = %+v", data)
	}
}
