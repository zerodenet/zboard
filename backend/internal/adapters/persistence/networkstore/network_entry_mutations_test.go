package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func networkEntryMutationService(db *gorm.DB) network.NetworkEntryMutations {
	return network.NetworkEntryMutations{
		Repository: NetworkEntryMutations{DB: db}, Cipher: proxyPoolCipher{},
		Inspector: proxyPoolInspectorFunc(func(_ context.Context, _ string, _ bool) (network.ProxyPoolConfigurationFacts, error) {
			return network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC) },
	}
}

func seedNetworkEntryMutationFixture(t *testing.T, db *gorm.DB) (model.ProtocolEndpoint, model.NodeGroup) {
	t.Helper()
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"is_admin": true, "status": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Node{}).Where("id = ?", 1).Updates(map[string]any{"is_enabled": true, "lifecycle_status": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{ID: 2, Name: "landing", Config: "{}", IsEnabled: true, LifecycleStatus: "active"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "vless", RuntimeKey: "network-entry-mutation", Protocol: "vless", Port: 443, PublicPort: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "members", Code: "members", IsEnabled: true, Revision: 1}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	subscription := model.Subscription{UserID: 1, NodeGroupID: group.ID, StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(time.Hour), Status: "active", FlowTotal: 1024, FlowUsed: 0}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	return endpoint, group
}

func TestNetworkEntryMutationsCommitEntryMembershipTaskAuditAndPublicationsAtomically(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, group := seedNetworkEntryMutationFixture(t, db)
	service := networkEntryMutationService(db)
	result, err := service.Save(context.Background(), 1, network.NetworkEntryMutationRequest{
		NodeID: 1, EndpointID: endpoint.ID, Name: "front", Network: "tcp_udp", Address: "front.example.test",
		Port: 1443, PublicPort: 1443, Enabled: true, CredentialProtocols: []string{"vless"},
		MembershipChanges: []network.NetworkEntryMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: 1, Member: true}},
	})
	if err != nil || result.Entry.ID == 0 || result.Entry.Revision != 1 || len(result.ReconcileTaskIDs) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var storedGroup model.NodeGroup
	if err := db.First(&storedGroup, group.ID).Error; err != nil || storedGroup.Revision != 2 {
		t.Fatalf("group=%+v error=%v", storedGroup, err)
	}
	var membership model.NodeGroupNetworkEntry
	if err := db.Where("node_group_id = ? AND network_entry_id = ?", group.ID, result.Entry.ID).First(&membership).Error; err != nil {
		t.Fatal(err)
	}
	var task model.Task
	if err := db.First(&task, result.ReconcileTaskIDs[0]).Error; err != nil || task.IdempotencyKey != fmt.Sprintf("node-group-reconcile:%d:2", group.ID) || task.ScheduledAt == nil {
		t.Fatalf("task=%+v error=%v", task, err)
	}
	var content network.BatchOperationContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil || content.RequestedBy != 1 || content.NodeGroupID != group.ID || len(content.EndpointIDsByNode["2"]) != 1 {
		t.Fatalf("content=%+v error=%v", content, err)
	}
	var items []model.TaskItem
	if err := db.Where("task_id = ?", task.ID).Order("id").Find(&items).Error; err != nil || len(items) != 2 || items[0].TargetType != "node_group" || items[1].TargetType != "node" {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	var publications []model.NodeConfigPublish
	if err := db.Order("node_id").Find(&publications).Error; err != nil || len(publications) != 2 || publications[0].NodeID != 1 || publications[1].NodeID != 2 {
		t.Fatalf("publications=%+v error=%v", publications, err)
	}
	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("action IN ?", []string{"network_entry.create", "task.create"}).Count(&audits).Error; err != nil || audits != 2 {
		t.Fatalf("audits=%d error=%v", audits, err)
	}
}

func TestNetworkEntryMutationConflictPermissionAndAuditFailureRollBackWholeGraph(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, group := seedNetworkEntryMutationFixture(t, db)
	service := networkEntryMutationService(db)
	request := network.NetworkEntryMutationRequest{
		NodeID: 1, EndpointID: endpoint.ID, Name: "front", Network: "tcp_udp", Address: "front.example.test",
		Port: 1443, PublicPort: 1443, Enabled: true, CredentialProtocols: []string{"vless"},
		MembershipChanges: []network.NetworkEntryMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: 99, Member: true}},
	}
	if _, err := service.Save(context.Background(), 1, request); !errors.Is(err, network.ErrNetworkEntryConflict) {
		t.Fatalf("group conflict error=%v", err)
	}
	for name, value := range map[string]any{"entries": &model.NetworkEntry{}, "memberships": &model.NodeGroupNetworkEntry{}, "tasks": &model.Task{}, "publishes": &model.NodeConfigPublish{}} {
		var count int64
		if err := db.Model(value).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", name, count, err)
		}
	}
	request.MembershipChanges[0].ExpectedRevision = 1
	if err := db.Callback().Create().Before("gorm:create").Register("fail_network_entry_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(context.Background(), 1, request); err == nil {
		t.Fatal("audit failure accepted")
	}
	if err := db.Callback().Create().Remove("fail_network_entry_audit"); err != nil {
		t.Fatal(err)
	}
	var entries int64
	if err := db.Model(&model.NetworkEntry{}).Count(&entries).Error; err != nil || entries != 0 {
		t.Fatalf("entry escaped audit rollback: count=%d error=%v", entries, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(context.Background(), 1, request); !errors.Is(err, network.ErrNetworkEntryPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
}
