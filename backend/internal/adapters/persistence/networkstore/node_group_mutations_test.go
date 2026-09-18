package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func nodeGroupMutationService(db *gorm.DB) network.NodeGroupMutations {
	return network.NodeGroupMutations{Repository: NodeGroupMutations{DB: db}, Now: func() time.Time {
		return time.Date(2026, 9, 16, 12, 30, 0, 0, time.UTC)
	}}
}

func TestNodeGroupMutationsPersistRelationsTasksAuditsAndRevision(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, existing := seedNetworkEntryMutationFixture(t, db)
	service := nodeGroupMutationService(db)

	created, err := service.Create(context.Background(), 1, network.NodeGroupCreateRequest{
		Name: " Created ", Code: " CREATED ", ProtocolEndpointIDs: []uint{endpoint.ID, endpoint.ID},
		CredentialProtocols: []string{"vless"},
	})
	if err != nil || created.NodeGroup.ID == 0 || created.NodeGroup.Revision != 1 || created.ReconcileTask == nil {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	if len(created.NodeGroup.ProtocolEndpointIDs) != 1 || created.NodeGroup.ProtocolEndpointIDs[0] != endpoint.ID || created.NodeGroup.NetworkEntryIDs == nil {
		t.Fatalf("created relations=%+v", created.NodeGroup)
	}
	if created.ReconcileTask.IdempotencyKey != "node-group-reconcile:"+uintString(created.NodeGroup.ID)+":1" || created.ReconcileTask.ScheduledAt == nil || created.ReconcileTask.Total != 1 {
		t.Fatalf("create task=%+v", created.ReconcileTask)
	}

	revision := existing.Revision
	endpointIDs := []uint{endpoint.ID}
	updated, err := service.Update(context.Background(), 1, network.NodeGroupUpdateRequest{
		ID: existing.ID, ExpectedRevision: &revision, ProtocolEndpointIDs: &endpointIDs,
		CredentialProtocols: []string{"vless"},
	})
	if err != nil || updated.NodeGroup.Revision != 2 || updated.ReconcileTask == nil || updated.ReconcileTask.Total != 2 {
		t.Fatalf("updated=%+v error=%v", updated, err)
	}
	var content network.BatchOperationContent
	if err := json.Unmarshal([]byte(updated.ReconcileTask.Content), &content); err != nil || content.NodeGroupID != existing.ID || len(content.EndpointIDsByNode["2"]) != 1 {
		t.Fatalf("content=%+v error=%v", content, err)
	}
	var items []model.TaskItem
	if err := db.Where("task_id = ?", updated.ReconcileTask.ID).Order("id").Find(&items).Error; err != nil || len(items) != 2 || items[0].TargetType != "node_group" || items[1].TargetType != "node" {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("action IN ?", []string{"node_group.create", "node_group.update", "task.create"}).Count(&auditCount).Error; err != nil || auditCount != 4 {
		t.Fatalf("audits=%d error=%v", auditCount, err)
	}

	description := "scalar only"
	revision = updated.NodeGroup.Revision
	scalar, err := service.Update(context.Background(), 1, network.NodeGroupUpdateRequest{ID: existing.ID, ExpectedRevision: &revision, Description: &description})
	if err != nil || scalar.NodeGroup.Revision != 3 || scalar.ReconcileTask != nil || scalar.NodeGroup.Description != description {
		t.Fatalf("scalar=%+v error=%v", scalar, err)
	}
}

func TestNodeGroupMutationRollbackConflictAndPermission(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, group := seedNetworkEntryMutationFixture(t, db)
	service := nodeGroupMutationService(db)
	revision := group.Revision
	endpointIDs := []uint{endpoint.ID}
	var tasksBefore int64
	if err := db.Model(&model.Task{}).Count(&tasksBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject_node_group_audit", func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "node_group.update" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := service.Update(context.Background(), 1, network.NodeGroupUpdateRequest{ID: group.ID, ExpectedRevision: &revision, ProtocolEndpointIDs: &endpointIDs, CredentialProtocols: []string{"vless"}})
	if err == nil {
		t.Fatal("audit failure committed")
	}
	if err := db.Callback().Create().Remove("test:reject_node_group_audit"); err != nil {
		t.Fatal(err)
	}
	var after model.NodeGroup
	if err := db.First(&after, group.ID).Error; err != nil || after.Revision != revision {
		t.Fatalf("group=%+v error=%v", after, err)
	}
	var memberships, tasksAfter int64
	if err := db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Count(&memberships).Error; err != nil || memberships != 0 {
		t.Fatalf("memberships=%d error=%v", memberships, err)
	}
	if err := db.Model(&model.Task{}).Count(&tasksAfter).Error; err != nil || tasksAfter != tasksBefore {
		t.Fatalf("tasks=%d want=%d error=%v", tasksAfter, tasksBefore, err)
	}

	snapshot, err := (NodeGroupMutations{DB: db}).LoadNodeGroupMutation(context.Background(), 1, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NodeGroup{}).Where("id = ?", group.ID).Update("revision", revision+1).Error; err != nil {
		t.Fatal(err)
	}
	_, err = (NodeGroupMutations{DB: db}).CommitNodeGroupMutation(context.Background(), 1, &snapshot, network.NodeGroupMutationChange{Group: snapshot.Group, Now: time.Now().UTC()})
	var conflict *network.NodeGroupMutationConflict
	if !errors.As(err, &conflict) || conflict.CurrentRevision != revision+1 {
		t.Fatalf("conflict=%v error=%v", conflict, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), 1, network.NodeGroupUpdateRequest{ID: group.ID, ExpectedRevision: &revision, Description: stringPointerStore("blocked")}); !errors.Is(err, network.ErrNodeGroupMutationPermission) {
		t.Fatalf("permission error=%v", err)
	}
}

func TestNodeGroupMutationEntryGrantQueuesLandingPublicationAndRejectsInvalidEntry(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, _ := seedNetworkEntryMutationFixture(t, db)
	entry := model.NetworkEntry{Name: "front", Network: "tcp_udp", NodeID: 1, EndpointID: endpoint.ID, Address: "front.example.test", Port: 1666, PublicPort: 1666, Enabled: true, Revision: 1}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1 = 1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	service := nodeGroupMutationService(db)
	result, err := service.Create(context.Background(), 1, network.NodeGroupCreateRequest{Name: "front only", Code: "front-only", NetworkEntryIDs: []uint{entry.ID}, CredentialProtocols: []string{"vless"}})
	if err != nil || len(result.NodeGroup.ProtocolEndpointIDs) != 0 || len(result.NodeGroup.NetworkEntryIDs) != 1 || result.NodeGroup.NetworkEntryIDs[0] != entry.ID {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var publication model.NodeConfigPublish
	if err := db.First(&publication, "node_id = ?", endpoint.NodeID).Error; err != nil || publication.EndpointID != endpoint.ID || publication.RequestedBy != 0 {
		t.Fatalf("publication=%+v error=%v", publication, err)
	}
	_, err = service.Create(context.Background(), 1, network.NodeGroupCreateRequest{Name: "invalid", Code: "invalid-entry", NetworkEntryIDs: []uint{999999}})
	var validation *network.NodeGroupMutationValidation
	if !errors.As(err, &validation) || validation.Fields["network_entry_ids"] == "" {
		t.Fatalf("validation=%+v error=%v", validation, err)
	}
	var invalidCount int64
	if err := db.Model(&model.NodeGroup{}).Where("code = ?", "invalid-entry").Count(&invalidCount).Error; err != nil || invalidCount != 0 {
		t.Fatalf("invalid group count=%d error=%v", invalidCount, err)
	}
}

func uintString(value uint) string            { return strconv.FormatUint(uint64(value), 10) }
func stringPointerStore(value string) *string { return &value }
