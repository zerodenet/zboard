package networkstore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestBatchScopesResolveAndGroupInOneQueryEach(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC()
	nodes := []model.Node{
		{ID: 1, Name: "Tokyo edge", Address: "one.example.test", Region: "jp", Config: "{}", LifecycleStatus: "active", IsEnabled: true, ConnectorLastSeenAt: &now},
		{ID: 2, Name: "Retired", Address: "two.example.test", Region: "us", Config: "{}", LifecycleStatus: "retired"},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []model.ProtocolEndpoint{
		{ID: 10, NodeID: 1, Name: "alpha", RuntimeKey: "00000000-0000-4000-8000-000000000010", Protocol: "vless", Port: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true},
		{ID: 11, NodeID: 1, Name: "beta", RuntimeKey: "00000000-0000-4000-8000-000000000011", Protocol: "trojan", Port: 444, OptionalConfig: "{}", Tags: "[]", IsActive: false},
	}
	if err := db.Create(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolDeployment{ProtocolEndpointID: 10, NodeID: 1, Status: "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
	counter := &networkEntryQueryCounter{}
	store := BatchScopes{DB: db.Session(&gorm.Session{Logger: counter})}
	nodeIDs, err := store.ResolveBatchNodes(context.Background(), true, nil, network.BatchNodeFilter{Query: "tokyo", LifecycleStatus: "active"}, now.Add(-time.Minute), 10)
	if err != nil || len(nodeIDs) != 1 || nodeIDs[0] != 1 {
		t.Fatalf("nodes=%v err=%v", nodeIDs, err)
	}
	protocolIDs, err := store.ResolveBatchProtocols(context.Background(), true, nil, network.BatchProtocolFilter{DeploymentStatus: "never"}, 10)
	if err != nil || len(protocolIDs) != 1 || protocolIDs[0] != 11 {
		t.Fatalf("protocols=%v err=%v", protocolIDs, err)
	}
	groupedNodes, groups, err := store.GroupBatchProtocols(context.Background(), []uint{11, 10})
	if err != nil || len(groupedNodes) != 1 || len(groups["1"]) != 2 || groups["1"][0] != 10 {
		t.Fatalf("nodes=%v groups=%v err=%v", groupedNodes, groups, err)
	}
	if got := counter.count.Load(); got != 3 {
		t.Fatalf("three scope operations used %d queries, want 3", got)
	}
}
