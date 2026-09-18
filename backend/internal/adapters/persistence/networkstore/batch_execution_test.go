package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type resourceBatchAdapterStub struct {
	validationErr error
	publishErr    error
	validated     [][]uint
	published     []network.BatchResourceAction
	detected      []network.BatchResourceAction
	groups        []network.BatchResourceAction
}

func (s *resourceBatchAdapterStub) DetectBatchNode(_ context.Context, action network.BatchResourceAction) error {
	s.detected = append(s.detected, action)
	return nil
}
func (s *resourceBatchAdapterStub) ReconcileBatchNode(_ context.Context, _ network.BatchResourceAction) error {
	return nil
}
func (s *resourceBatchAdapterStub) ReconcileBatchNodeGroup(_ context.Context, action network.BatchResourceAction) error {
	s.groups = append(s.groups, action)
	return nil
}
func (s *resourceBatchAdapterStub) ValidateBatchProtocolActivation(_ context.Context, ids []uint) error {
	s.validated = append(s.validated, append([]uint(nil), ids...))
	return s.validationErr
}
func (s *resourceBatchAdapterStub) PublishBatchNodeConfig(_ context.Context, action network.BatchResourceAction) error {
	s.published = append(s.published, action)
	return s.publishErr
}

func seedResourceBatch(t *testing.T, db *gorm.DB, kind string, content network.BatchOperationContent, node model.Node, endpoints ...model.ProtocolEndpoint) network.BatchResourceClaim {
	t.Helper()
	if err := db.Create(&model.User{ID: content.RequestedBy, Email: "current-admin@example.test", Password: "hash", IsAdmin: true, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	for index := range endpoints {
		if err := db.Create(&endpoints[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(time.Minute)
	task := model.Task{Type: kind, Content: string(raw), Status: 1, Total: 1, LockedBy: "lease", LockedUntil: &until, MaxAttempts: 3}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "node", TargetID: "1", Payload: "{}", Status: 1, Attempts: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return network.BatchResourceClaim{TaskID: task.ID, ItemID: item.ID, Token: "lease"}
}

func resourceBatchService(db *gorm.DB, adapter network.BatchResourceAdapter) network.BatchResourceExecution {
	return network.BatchResourceExecution{Repository: BatchResourceExecution{DB: db}, Adapter: adapter}
}

func TestBatchNodeLifecycleChecksCurrentAuthorityAndRollsBackAuditFailure(t *testing.T) {
	for _, scenario := range []string{"success", "revoked", "audit"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			claim := seedResourceBatch(t, db, network.BatchNodeLifecycle, network.BatchOperationContent{RequestedBy: 1, Actor: "stale@example.test", LifecycleStatus: "maintenance"}, model.Node{ID: 1, Name: "node", Config: "{}", LifecycleStatus: "active", IsEnabled: true})
			if scenario == "revoked" {
				if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "audit" {
				if err := db.Callback().Create().Before("gorm:create").Register("fail_batch_resource_audit", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
						tx.AddError(errors.New("audit failed"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Create().Remove("fail_batch_resource_audit")
			}
			adapter := &resourceBatchAdapterStub{}
			err := resourceBatchService(db, adapter).Execute(context.Background(), claim)
			var node model.Node
			if loadErr := db.First(&node, 1).Error; loadErr != nil {
				t.Fatal(loadErr)
			}
			if scenario == "success" {
				if err != nil || node.LifecycleStatus != "maintenance" || node.IsEnabled || len(adapter.published) != 0 {
					t.Fatalf("lifecycle result node=%+v publications=%d err=%v", node, len(adapter.published), err)
				}
				var audit model.AuditLog
				if err := db.Where("action = ?", "node.lifecycle.batch").First(&audit).Error; err != nil || audit.Actor != "current-admin@example.test" {
					t.Fatalf("authoritative audit missing: %+v err=%v", audit, err)
				}
			} else if err == nil || node.LifecycleStatus != "active" || !node.IsEnabled {
				t.Fatalf("unauthorized or unaudited change committed: node=%+v err=%v", node, err)
			}
		})
	}
}

func TestBatchNodeDetectionUsesLockedTaskWithoutReloadingIt(t *testing.T) {
	db, _ := administrationFixture(t)
	claim := seedResourceBatch(t, db, network.BatchNodeDetect, network.BatchOperationContent{RequestedBy: 1}, model.Node{ID: 1, Name: "node", Config: "{}"})
	counter := &networkEntryQueryCounter{}
	adapter := &resourceBatchAdapterStub{}
	counted := db.Session(&gorm.Session{Logger: counter})
	if err := resourceBatchService(counted, adapter).Execute(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 3 {
		t.Fatalf("node detection admission used %d queries, want lease + item + current actor", got)
	}
	if len(adapter.detected) != 1 || adapter.detected[0].ActorID != 1 || adapter.detected[0].NodeID != 1 {
		t.Fatalf("detection action = %+v", adapter.detected)
	}
}

func TestBatchNodeGroupExecutionUsesPersistedTargetAndCurrentAuthority(t *testing.T) {
	db, _ := administrationFixture(t)
	claim := seedResourceBatch(t, db, network.BatchNodeGroupSync, network.BatchOperationContent{RequestedBy: 1, NodeGroupID: 9}, model.Node{ID: 1, Name: "node", Config: "{}"})
	if err := db.Model(&model.TaskItem{}).Where("id = ?", claim.ItemID).Updates(map[string]any{"target_type": "node_group", "target_id": "9"}).Error; err != nil {
		t.Fatal(err)
	}
	adapter := &resourceBatchAdapterStub{}
	if err := resourceBatchService(db, adapter).Execute(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if len(adapter.groups) != 1 || adapter.groups[0].NodeGroupID != 9 || adapter.groups[0].ActorID != 1 {
		t.Fatalf("group action = %+v", adapter.groups)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := resourceBatchService(db, &resourceBatchAdapterStub{}).Execute(context.Background(), claim); !errors.Is(err, network.ErrResourcePermission) {
		t.Fatalf("revoked actor executed group reconciliation: %v", err)
	}
}

func TestBatchProtocolActivationCommitsBeforePublishingAndRejectsOldLease(t *testing.T) {
	db, _ := administrationFixture(t)
	active := true
	claim := seedResourceBatch(t, db, network.BatchProtocolActive, network.BatchOperationContent{RequestedBy: 1, IsActive: &active, EndpointIDsByNode: map[string][]uint{"1": {10}}}, model.Node{ID: 1, Name: "node", Config: "{}"}, model.ProtocolEndpoint{ID: 10, NodeID: 1, Name: "endpoint", RuntimeKey: "00000000-0000-4000-8000-000000000010", Protocol: "vless", Port: 443, OptionalConfig: "{}", Tags: "[]", IsActive: false})
	publishErr := errors.New("remote publication failed")
	adapter := &resourceBatchAdapterStub{publishErr: publishErr}
	err := resourceBatchService(db, adapter).Execute(context.Background(), claim)
	if !errors.Is(err, publishErr) || len(adapter.validated) != 1 || len(adapter.published) != 1 || adapter.published[0].PublishEndpointID != 10 {
		t.Fatalf("activation boundary err=%v validations=%d publications=%+v", err, len(adapter.validated), adapter.published)
	}
	var endpoint model.ProtocolEndpoint
	if err := db.First(&endpoint, 10).Error; err != nil || !endpoint.IsActive {
		t.Fatalf("committed activation was lost after publication error: active=%t err=%v", endpoint.IsActive, err)
	}
	claim.Token = "old-lease"
	if err := resourceBatchService(db, &resourceBatchAdapterStub{}).Execute(context.Background(), claim); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("old lease executed resource action: %v", err)
	}
}

func TestBatchProtocolDisableProtectsActivePlans(t *testing.T) {
	db, _ := administrationFixture(t)
	active := false
	claim := seedResourceBatch(t, db, network.BatchProtocolActive, network.BatchOperationContent{RequestedBy: 1, IsActive: &active, EndpointIDsByNode: map[string][]uint{"1": {10}}}, model.Node{ID: 1, Name: "node", Config: "{}"}, model.ProtocolEndpoint{ID: 10, NodeID: 1, Name: "endpoint", RuntimeKey: "00000000-0000-4000-8000-000000000010", Protocol: "vless", Port: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true})
	if err := db.Create(&model.NodeGroup{ID: 1, Name: "group", Code: "group", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeGroupEndpoint{NodeGroupID: 1, ProtocolEndpointID: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Plan{ID: 1, Name: "plan", Slug: "plan", NodeGroupID: 1, IsActive: true, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	adapter := &resourceBatchAdapterStub{}
	err := resourceBatchService(db, adapter).Execute(context.Background(), claim)
	var endpoint model.ProtocolEndpoint
	if loadErr := db.First(&endpoint, 10).Error; loadErr != nil {
		t.Fatal(loadErr)
	}
	if err == nil || !endpoint.IsActive || len(adapter.published) != 0 {
		t.Fatalf("active-plan endpoint was disabled: active=%t publications=%d err=%v", endpoint.IsActive, len(adapter.published), err)
	}
}
