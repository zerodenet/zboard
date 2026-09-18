package networkstore

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type inventoryQueryCounter struct {
	logger.Interface
	count atomic.Int64
}

func (c *inventoryQueryCounter) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	c.count.Add(1)
	c.Interface.Trace(ctx, begin, query, err)
}

func TestRuntimeNodesUsesOneBatchQuery(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "network-inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	nodes := []model.Node{
		{Name: "batch-a", Config: "{}", IsEnabled: true, NodeCredential: "secret-a"},
		{Name: "batch-b", Config: "{}", IsEnabled: true, NodeCredential: "secret-b"},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	counter := &inventoryQueryCounter{Interface: db.Logger}
	queryDB := db.Session(&gorm.Session{Logger: counter})
	rows, err := (Inventory{DB: queryDB}).RuntimeNodes(context.Background(), []uint{nodes[0].ID, nodes[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 1 {
		t.Fatalf("runtime node query count = %d, want 1", got)
	}
	if rows[nodes[0].ID].NodeCredentialCiphertext != "secret-a" || rows[nodes[1].ID].NodeCredentialCiphertext != "secret-b" {
		t.Fatalf("runtime nodes = %+v", rows)
	}
}

func TestProtocolEndpointPageReturnsDeploymentStatusFacetsWithoutChangingSelectedTotal(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "protocol-facets.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "facets", Address: "192.0.2.1", Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []model.ProtocolEndpoint{
		{NodeID: node.ID, Name: "never", RuntimeKey: "facet-never", Protocol: "vless", Address: node.Address, Port: 1001, PublicPort: 1001, IsActive: true},
		{NodeID: node.ID, Name: "success", RuntimeKey: "facet-success", Protocol: "vless", Address: node.Address, Port: 1002, PublicPort: 1002, IsActive: true},
		{NodeID: node.ID, Name: "running", RuntimeKey: "facet-running", Protocol: "vless", Address: node.Address, Port: 1003, PublicPort: 1003, IsActive: true},
		{NodeID: node.ID, Name: "failed", RuntimeKey: "facet-failed", Protocol: "vless", Address: node.Address, Port: 1004, PublicPort: 1004, IsActive: true},
	}
	if err := db.Create(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	deployments := []model.ProtocolDeployment{
		{ProtocolEndpointID: endpoints[1].ID, NodeID: node.ID, Status: "failed"},
		{ProtocolEndpointID: endpoints[1].ID, NodeID: node.ID, Status: "succeeded"},
		{ProtocolEndpointID: endpoints[2].ID, NodeID: node.ID, Status: "running"},
		{ProtocolEndpointID: endpoints[3].ID, NodeID: node.ID, Status: "failed"},
	}
	if err := db.Create(&deployments).Error; err != nil {
		t.Fatal(err)
	}
	counter := &inventoryQueryCounter{Interface: db.Logger}
	store := Inventory{DB: db.Session(&gorm.Session{Logger: counter})}
	page, err := store.ListProtocolEndpoints(context.Background(), network.ProtocolEndpointInventoryQuery{Paged: true, IncludeStatusFacets: true, Limit: 1, DeploymentStatus: "succeeded", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Endpoint.ID != endpoints[1].ID {
		t.Fatalf("selected page = %+v", page)
	}
	want := network.ProtocolEndpointStatusFacets{All: 4, Succeeded: 1, Running: 1, Failed: 1, Never: 1}
	if page.Facets != want {
		t.Fatalf("facets = %+v, want %+v", page.Facets, want)
	}
	if got := counter.count.Load(); got != 9 {
		t.Fatalf("protocol endpoint page query count = %d, want 9", got)
	}
}

func TestProtocolUsageReadsBoundedDailyProjectionInsteadOfLedger(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "protocol-usage-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&model.TrafficRecord{ProtocolEndpointID: 9, ReportID: "raw-ledger", Nonce: "raw-ledger", UsedBytes: 9999, At: now}).Error; err != nil {
		t.Fatal(err)
	}
	rollups := []model.ProtocolEndpointUsageDaily{
		{ProtocolEndpointID: 9, UsageDate: "2026-09-17", UsedBytes: 31, RecordCount: 1, LastRecordAt: now.AddDate(0, 0, -1), UpdatedAt: now},
		{ProtocolEndpointID: 9, UsageDate: "2026-09-18", UsedBytes: 47, RecordCount: 2, LastRecordAt: now, UpdatedAt: now},
	}
	if err := db.Create(&rollups).Error; err != nil {
		t.Fatal(err)
	}
	usage, err := (Inventory{DB: db}).ProtocolUsage(context.Background(), []uint{9}, now)
	if err != nil {
		t.Fatal(err)
	}
	if usage[9].UsedBytesToday != 47 || usage[9].UsedBytesTotal != 78 {
		t.Fatalf("usage read raw ledger instead of projection: %+v", usage[9])
	}
}
