package networkstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func nodeRemovalFixtureService(db *gorm.DB) network.NodeRemoval {
	return network.NodeRemoval{Store: NodeRemoval{DB: db}}
}
func TestNodeRemovalPreservesChildExecutionEvidence(t *testing.T) {
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := administrationFixture(t)
			prepareProviderDirectory(t, db)
			var opID uint
			resource := "dns:7"
			if kind == "dns_operation" {
				op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, Status: "failed", OperationType: "sync"}
				if err := db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID = op.ID
			} else {
				op := model.CertificateOperation{ManagedCertificateID: 9, NodeID: 1, Status: "failed", OperationType: "issue"}
				if err := db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID = op.ID
				resource = "certificate:9"
			}
			service := nodeRemovalFixtureService(db)
			run := submit(t, jobstore.New(db), fmt.Sprintf("%s:%d", kind, opID), kind, resource)
			for _, state := range []string{"queued", "running", "unknown", "succeeded"} {
				if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", state).Error; err != nil {
					t.Fatal(err)
				}
				var blocked *network.ResourceRemovalBlocked
				if _, err := service.Remove(context.Background(), 1, 1); !errors.As(err, &blocked) {
					t.Fatal("child evidence removed", state, err)
				}
			}
			if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", "failed").Error; err != nil {
				t.Fatal(err)
			}
			result, err := service.Remove(context.Background(), 1, 1)
			if err != nil || !result.Deleted || !result.RemoteZeroRetained || !result.ProviderDNSRecordsRetained {
				t.Fatal(result, err)
			}
			var retained jobstore.Record
			if err := db.First(&retained, "id = ?", run.ID).Error; err != nil {
				t.Fatal("Run removed", err)
			}
		})
	}
}
func TestNodeRemovalRejectsExpiredPublicationLeaseAndRunningTaskItem(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	service := nodeRemovalFixtureService(db)
	old := time.Now().UTC().Add(-time.Hour)
	publication := model.NodeConfigPublish{NodeID: 1, Generation: 1, LeaseToken: "expired-but-unverified", LeaseUntil: old, NextAttemptAt: old}
	if err := db.Create(&publication).Error; err != nil {
		t.Fatal(err)
	}
	var blocked *network.ResourceRemovalBlocked
	if _, err := service.Remove(context.Background(), 1, 1); !errors.As(err, &blocked) || blocked.Blockers["node_publication"] != 1 {
		t.Fatal("expired lease assumed complete", err)
	}
	if err := db.Model(&publication).Update("lease_token", "").Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "test", Scope: "{}", Content: "{}", Status: 3, IdempotencyKey: "node-removal"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := model.TaskItem{TaskID: task.ID, TargetType: "node", TargetID: "1", Payload: "{}", Status: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Remove(context.Background(), 1, 1); !errors.As(err, &blocked) || blocked.Blockers["admin_tasks"] != 1 {
		t.Fatal("failed parent hid running task item", err)
	}
	if err := db.Model(&item).Update("status", 3).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Remove(context.Background(), 1, 1); err != nil {
		t.Fatal(err)
	}
}
func TestNodeRemovalRollsBackLifecycleAndProjectionWrites(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	seedNodeRemovalFlows(t, db)
	var before model.Node
	if err := db.First(&before, 1).Error; err != nil {
		t.Fatal(err)
	}
	for _, failAudit := range []bool{false, true} {
		if !failAudit {
			if err := db.Callback().Delete().Before("gorm:delete").Register("fail_node_projection", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "principal_flow_node_generations" {
					tx.AddError(errors.New("projection unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
		if failAudit {
			if err := db.Callback().Create().Before("gorm:create").Register("fail_node_audit", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
					tx.AddError(errors.New("audit failed"))
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
		service := network.NodeRemoval{Store: NodeRemoval{DB: db}}
		if _, err := service.Remove(context.Background(), 1, 1); err == nil {
			t.Fatal("partial removal committed")
		}
		if failAudit {
			db.Callback().Create().Remove("fail_node_audit")
		} else {
			db.Callback().Delete().Remove("fail_node_projection")
		}
		var current meteringstore.PrincipalFlowCurrent
		if err := db.Where("node_id = ?", 1).First(&current).Error; err != nil || current.ActiveFlows != 4 {
			t.Fatal("projection deletion escaped rollback", current, err)
		}
		var scopes int64
		if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Count(&scopes).Error; err != nil || scopes != 0 {
			t.Fatal("projection history escaped rollback", scopes, err)
		}
		var after model.Node
		if err := db.First(&after, 1).Error; err != nil {
			t.Fatal(err)
		}
		if after.Name != before.Name || after.LifecycleStatus != before.LifecycleStatus || after.IsEnabled != before.IsEnabled {
			t.Fatal("failed removal changed node lifecycle")
		}
		var count int64
		if err := db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", 1).Count(&count).Error; err != nil || count != 2 {
			t.Fatal(count, err)
		}
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := nodeRemovalFixtureService(db).Remove(context.Background(), 1, 1); !errors.Is(err, network.ErrResourcePermission) {
		t.Fatal(err)
	}
}

func TestLedgerCoordinationDefersSnapshotUntilDomainLock(t *testing.T) {
	db, other := administrationFixture(t)
	if db.Dialector.Name() != "mysql" {
		t.Skip("requires MySQL repeatable-read snapshots")
	}
	prepareProviderDirectory(t, db)
	var isolation string
	if err := db.Raw("SELECT @@transaction_isolation").Scan(&isolation).Error; err != nil {
		t.Fatal(err)
	}
	if isolation != "REPEATABLE-READ" {
		t.Fatalf("unexpected isolation %q", isolation)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	locked, changed := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- jobstore.New(db).WithLedgerLock(ctx, func(tx *gorm.DB) error {
			close(locked)
			select {
			case <-changed:
			case <-ctx.Done():
				return ctx.Err()
			}
			var current model.Node
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error; err != nil {
				return err
			}
			var snapshot model.Node
			if err := tx.First(&snapshot, 1).Error; err != nil {
				return err
			}
			if current.Name != "concurrent-update" || snapshot.Name != current.Name {
				return fmt.Errorf("stale domain snapshot: current=%s snapshot=%s", current.Name, snapshot.Name)
			}
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	err := other.WithContext(ctx).Model(&model.Node{}).Where("id = ?", 1).Update("name", "concurrent-update").Error
	close(changed)
	if result := <-done; result != nil {
		t.Fatal(result)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func seedNodeRemovalFlows(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	for _, row := range []meteringstore.PrincipalFlowCurrent{
		{NodeID: 1, PrincipalKey: "removed", UserID: 1, SubscriptionID: 7, ActiveFlows: 4, ObservedAt: now, UpdatedAt: now},
		{NodeID: 2, PrincipalKey: "retained", UserID: 1, SubscriptionID: 7, ActiveFlows: 6, ObservedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	generation := meteringstore.PrincipalFlowNodeGeneration{NodeID: 1, CoreInstanceID: "core", StartedAt: now, UpdatedAt: now}
	if err := db.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
}
func TestNodeRemovalRefreshesSharedScopesAndRetainsHistory(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	seedNodeRemovalFlows(t, db)
	now := time.Now().UTC()
	history := meteringstore.PrincipalFlowObservation{NodeID: 1, CoreInstanceID: "core", PrincipalKey: "removed", EventID: "old", UserID: 1, ActiveFlows: 4, ObservedAt: now, CreatedAt: now}
	if err := db.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	result, err := nodeRemovalFixtureService(db).Remove(context.Background(), 1, 1)
	if err != nil || result.Cleanup.PrincipalFlowCurrents != 1 || result.Cleanup.PrincipalFlowGeneration != 1 {
		t.Fatal(result, err)
	}
	var scopes []meteringstore.PrincipalFlowScopeCurrent
	if err := db.Find(&scopes).Error; err != nil || len(scopes) != 2 {
		t.Fatal(scopes, err)
	}
	for _, scope := range scopes {
		if scope.ActiveFlows != 6 {
			t.Fatal("surviving node excluded", scope)
		}
	}
	var observations []meteringstore.PrincipalFlowScopeObservation
	if err := db.Find(&observations).Error; err != nil || len(observations) != 2 {
		t.Fatal(observations, err)
	}
	for _, row := range observations {
		if row.ActiveFlows != 6 || row.Source != "node_deleted" || row.NodeID != 1 {
			t.Fatal(row)
		}
	}
	if err := db.First(&history, history.ID).Error; err != nil {
		t.Fatal("historical observation removed", err)
	}
	var currents []meteringstore.PrincipalFlowCurrent
	if err := db.Find(&currents).Error; err != nil || len(currents) != 1 || currents[0].NodeID != 2 {
		t.Fatal(currents, err)
	}
}
