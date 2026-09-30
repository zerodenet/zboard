package networkstore

import (
	"context"
	"fmt"
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

func TestNodeInventoryCountsActiveEndpointsAndEnabledEntriesOnOwningNode(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "node-service-counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	nodes := []model.Node{
		{Name: "entry-node", Address: "192.0.2.10", Config: "{}", IsEnabled: true},
		{Name: "landing-node", Address: "192.0.2.20", Config: "{}", IsEnabled: true},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []model.ProtocolEndpoint{
		{NodeID: nodes[0].ID, Name: "entry-local", RuntimeKey: "entry-local", Protocol: "vless", Address: nodes[0].Address, Port: 1001, PublicPort: 1001, IsActive: true},
		{NodeID: nodes[0].ID, Name: "entry-disabled", RuntimeKey: "entry-disabled", Protocol: "vless", Address: nodes[0].Address, Port: 1002, PublicPort: 1002, IsActive: false},
		{NodeID: nodes[1].ID, Name: "landing", RuntimeKey: "landing", Protocol: "vless", Address: nodes[1].Address, Port: 2001, PublicPort: 2001, IsActive: true},
	}
	if err := db.Create(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoints[1].ID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	entries := []model.NetworkEntry{
		{Name: "front-enabled", Network: "tcp_udp", NodeID: nodes[0].ID, EndpointID: endpoints[2].ID, Address: nodes[0].Address, Port: 3001, PublicPort: 3001, Enabled: true},
		{Name: "front-disabled", Network: "tcp_udp", NodeID: nodes[0].ID, EndpointID: endpoints[2].ID, Address: nodes[0].Address, Port: 3002, PublicPort: 3002, Enabled: false},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NetworkEntry{}).Where("id = ?", entries[1].ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	page, err := (Inventory{DB: db}).ListNodes(context.Background(), network.NodeInventoryQuery{Paged: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[uint]int64, len(page.Items))
	for _, item := range page.Items {
		counts[item.Node.ID] = item.EnabledProtocolCount
	}
	if counts[nodes[0].ID] != 2 {
		t.Fatalf("entry node service count = %d, want active endpoint plus enabled entry", counts[nodes[0].ID])
	}
	if counts[nodes[1].ID] != 1 {
		t.Fatalf("landing node service count = %d, want only its active endpoint", counts[nodes[1].ID])
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
	if got := counter.count.Load(); got != 10 {
		t.Fatalf("protocol endpoint page query count = %d, want 10 including batched publication state", got)
	}
}

func TestProtocolPublicationQueueOverridesOldDeploymentSnapshot(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "publication-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	node := model.Node{Name: "queued", Address: "192.0.2.10", Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "service", RuntimeKey: "publication-state", Protocol: "vless", Address: node.Address, Port: 443, PublicPort: 443, IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolDeployment{NodeID: node.ID, ProtocolEndpointID: endpoint.ID, Status: "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
	request := model.NodeConfigPublish{NodeID: node.ID, EndpointID: endpoint.ID, Generation: 1, NextAttemptAt: now, LeaseUntil: now.Add(-time.Second)}
	if err := db.Create(&request).Error; err != nil {
		t.Fatal(err)
	}
	store := Inventory{DB: db}
	read := func() network.ProtocolEndpointInventoryItem {
		item, err := store.ProtocolEndpoint(context.Background(), endpoint.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	if item := read(); item.LatestDeployment == nil || item.LatestDeployment.Status != "succeeded" || item.Publication == nil || item.Publication.Status != "queued" {
		t.Fatalf("queued publication must be visible beside previous success: %+v", item)
	}
	page, err := store.ListProtocolEndpoints(context.Background(), network.ProtocolEndpointInventoryQuery{Paged: true, IncludeStatusFacets: true, Limit: 10, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if page.Facets.Queued != 1 || page.Facets.Succeeded != 0 {
		t.Fatalf("live publication facets must override previous success: %+v", page.Facets)
	}
	services, err := store.ListProtocolEndpoints(context.Background(), network.ProtocolEndpointInventoryQuery{ServiceKind: "all", Paged: true, IncludeStatusFacets: true, Limit: 10, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if services.Facets.Queued != 1 || services.Facets.Succeeded != 0 || len(services.Items) != 1 || services.Items[0].Publication == nil {
		t.Fatalf("combined service view lost pending publication: %+v", services)
	}
	front := model.Node{Name: "front", Address: "192.0.2.11", Config: "{}", IsEnabled: true}
	if err := db.Create(&front).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{Name: "entry", Network: "tcp", NodeID: front.ID, EndpointID: endpoint.ID, Address: front.Address, Port: 1443, PublicPort: 443, Enabled: true, Revision: 1}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	services, err = store.ListProtocolEndpoints(context.Background(), network.ProtocolEndpointInventoryQuery{ServiceKind: "all", Paged: true, IncludeStatusFacets: true, Limit: 10, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if services.Facets.Queued != 2 || services.Facets.Succeeded != 0 || len(services.Items) != 2 {
		t.Fatalf("forward service must inherit pending landing publication: %+v", services)
	}
	page, err = store.ListProtocolEndpoints(context.Background(), network.ProtocolEndpointInventoryQuery{Paged: true, Limit: 10, DeploymentStatus: "succeeded", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("queued endpoint was included in published filter: %+v", page)
	}
	if err := db.Model(&request).Updates(map[string]any{"lease_token": "worker", "lease_until": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if item := read(); item.Publication == nil || item.Publication.Status != "running" {
		t.Fatalf("running publication = %+v", item.Publication)
	}
	if err := db.Model(&request).Updates(map[string]any{"lease_token": "", "lease_until": now.Add(-time.Second), "last_error": "publish failed", "attempts": 1, "next_attempt_at": now.Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if item := read(); item.Publication == nil || item.Publication.Status != "failed" || item.Publication.Attempts != 1 {
		t.Fatalf("retrying publication = %+v", item.Publication)
	}
	if err := (PublicationRuntimeQueue{DB: db}).Retry(context.Background(), node.ID, 3, "admin@example.test", now); err != nil {
		t.Fatal(err)
	}
	if item := read(); item.Publication == nil || item.Publication.Status != "queued" || item.LatestDeployment == nil || item.LatestDeployment.Status != "succeeded" {
		t.Fatalf("manual retry must queue a new generation without rewriting history: %+v", item)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "node.publish.retry", fmt.Sprintf("node:%d", node.ID)).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("retry audit count = %d, error = %v", audits, err)
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
