package handler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type planGroupFixture struct {
	fullReplacementFixture
	group              model.NodeGroup
	old, shared, added model.ProtocolEndpoint
}

func TestPlanGroupSyncSupportsMoreThanOneThousand(t *testing.T) {
	assertPlanSyncPagedSuccess(t, newOrderFixture(t), true)
}
func TestMySQLPlanGroupSyncSupportsMoreThanOneThousand(t *testing.T) {
	assertPlanSyncPagedSuccess(t, newMySQLBusinessOrderFixture(t), true)
}

func TestMySQLPlanGroupPendingPaymentReadsMembershipCommittedAfterPlanWait(t *testing.T) {
	for _, forward := range []bool{false, true} {
		name := "direct"
		if forward {
			name = "forward"
		}
		t.Run(name, func(t *testing.T) {
			base := newMySQLBusinessOrderFixture(t)
			f := newFullReplacementFixture(t, base, false, 0, 0)
			admin := planQuotaRaceAdmin(t, f)
			pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "pending-new-before-late-membership"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			blocker := f.db.Begin()
			if blocker.Error != nil {
				t.Fatal(blocker.Error)
			}
			defer blocker.Rollback()
			var plan model.Plan
			if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, base.planRecord.ID).Error; err != nil {
				t.Fatal(err)
			}
			type paymentMarker struct{}
			var snapshot atomic.Bool
			const hook = "test:payment-before-late-group"
			if err := f.db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
				if q.Error == nil && q.RowsAffected > 0 && q.Statement.Context.Value(paymentMarker{}) == true && q.Statement.Table == "order_fundings" {
					snapshot.Store(true)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer f.db.Callback().Query().Remove(hook)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := f.payments.Pay(context.WithValue(ctx, paymentMarker{}, true), 1, pending.ID)
				done <- err
			}()
			waited := false
			for ctx.Err() == nil && !waited {
				var waiting int64
				if err := f.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=DATABASE()").Scan(&waiting).Error; err != nil {
					t.Fatal(err)
				}
				waited = snapshot.Load() && waiting > 0
				if !waited {
					select {
					case err := <-done:
						t.Fatalf("payment did not wait on plan after its old snapshot: %v", err)
					case <-time.After(time.Millisecond):
					}
				}
			}
			if !waited {
				t.Fatal("old payment read snapshot and actual plan lock wait were not established")
			}
			// Both the group and its membership are newer than the payment's RR
			// snapshot. No fabricated old-order snapshot is written by this test.
			group := model.NodeGroup{Name: "post-snapshot-group", Code: "post-snapshot-group", IsEnabled: true}
			if err := blocker.Create(&group).Error; err != nil {
				t.Fatal(err)
			}
			endpoint := planGroupEndpoint(t, blocker, "post-snapshot-endpoint")
			// Two endpoints share a landing node, with separate and overlapping
			// forward routes. Node-only dedup must retain every current identity.
			second := endpoint
			second.ID, second.RuntimeKey, second.Name, second.Port = 0, uuid.NewString(), "post-snapshot-second", 9443
			if err := blocker.Create(&second).Error; err != nil {
				t.Fatal(err)
			}
			publicationNodes := []uint{endpoint.NodeID}
			if err := blocker.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: second.ID}).Error; err != nil {
				t.Fatal(err)
			}
			for i, target := range []model.ProtocolEndpoint{endpoint, second, second} {
				front := model.Node{Name: fmt.Sprintf("post-snapshot-route-%d", i), Address: "route.example.test", Config: "{}"}
				if err := blocker.Create(&front).Error; err != nil {
					t.Fatal(err)
				}
				entry := model.NetworkEntry{NodeID: front.ID, EndpointID: target.ID, Name: fmt.Sprintf("route-%d", i), Network: "tcp", Address: "route.example.test", Port: 7443 + i, PublicPort: 7443 + i, Enabled: true}
				if err := blocker.Create(&entry).Error; err != nil {
					t.Fatal(err)
				}
				publicationNodes = append(publicationNodes, front.ID)
			}
			if forward {
				front := model.Node{Name: "post-snapshot-forward", Address: "forward.example.test", Config: "{}"}
				if err := blocker.Create(&front).Error; err != nil {
					t.Fatal(err)
				}
				entry := model.NetworkEntry{NodeID: front.ID, EndpointID: endpoint.ID, Name: "post-snapshot-entry", Network: "tcp", Address: "forward.example.test", Port: 8443, PublicPort: 8443, Enabled: true}
				if err := blocker.Create(&entry).Error; err != nil {
					t.Fatal(err)
				}
				if err := blocker.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
					t.Fatal(err)
				}
				publicationNodes = append(publicationNodes, front.ID)
			} else if err := blocker.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
				t.Fatal(err)
			}
			updater := commerce.PlanUpdate{Repository: commercestore.PlanUpdate{DB: blocker}}
			if _, err := updater.Update(t.Context(), admin, plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &group.ID, ExpectedRevision: &plan.Revision}); err != nil {
				t.Fatal(err)
			}
			generations := map[uint]uint64{}
			for _, node := range publicationNodes {
				var queue model.NodeConfigPublish
				if err := blocker.First(&queue, node).Error; err != nil {
					t.Fatal(err)
				}
				generations[node] = queue.Generation
			}
			if err := blocker.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("old pending payment failed after current group commit: %v", err)
			}
			var paid model.Order
			if err := f.db.First(&paid, pending.ID).Error; err != nil {
				t.Fatal(err)
			}
			var sub model.Subscription
			if err := f.db.First(&sub, paid.SubscriptionID).Error; err != nil || sub.NodeGroupID != group.ID {
				t.Fatal("payment resurrected old group", err)
			}
			var credential model.ProtocolCredential
			if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", sub.ID, endpoint.ID).First(&credential).Error; err != nil || !credential.DeliveryRequired {
				t.Fatal("old RR snapshot dropped new binding", err)
			}
			for _, node := range publicationNodes {
				var queue model.NodeConfigPublish
				if err := f.db.First(&queue, node).Error; err != nil || queue.Generation <= generations[node] {
					t.Fatalf("payment lost its own current-membership publication for node %d: %v", node, err)
				}
			}
		})
	}
}

func planGroupEndpoint(t *testing.T, db *gorm.DB, name string) model.ProtocolEndpoint {
	t.Helper()
	node := model.Node{Name: name, Address: name + ".example.test", Config: "{}"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: name, RuntimeKey: uuid.NewString(), Protocol: "vless", Address: name + ".example.test", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func newPlanGroupFixture(t *testing.T, base orderFixture) planGroupFixture {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 0, 0)
	var old model.ProtocolEndpoint
	if err := f.db.First(&old, f.oldCredential.ProtocolEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	shared := planGroupEndpoint(t, f.db, "plan-group-shared")
	added := planGroupEndpoint(t, f.db, "plan-group-added")
	encrypted, err := base.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []model.ProtocolEndpoint{old, shared, added} {
		if err := f.db.Model(&endpoint).Update("server_config", encrypted).Error; err != nil {
			t.Fatal(err)
		}
	}
	group := model.NodeGroup{Name: "New plan access", Code: "new-plan-access", IsEnabled: true}
	if err := f.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.NodeGroupEndpoint{{NodeGroupID: base.group.ID, ProtocolEndpointID: shared.ID}, {NodeGroupID: group.ID, ProtocolEndpointID: shared.ID}, {NodeGroupID: group.ID, ProtocolEndpointID: added.ID}} {
		if err := f.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := base.h.services.NodeCredentialReconciliation(base.h.credentialCipher, true).ReconcileNode(t.Context(), shared.NodeID); err != nil {
		t.Fatal(err)
	}
	return planGroupFixture{fullReplacementFixture: f, group: group, old: old, shared: shared, added: added}
}

func TestPlanGroupSyncPreservesSharedAndFencesOldDelivery(t *testing.T) {
	assertPlanGroupSyncDelivery(t, newOrderFixture(t))
}
func TestMySQLPlanGroupSyncPreservesSharedAndFencesOldDelivery(t *testing.T) {
	assertPlanGroupSyncDelivery(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanGroupSyncDelivery(t *testing.T, base orderFixture) {
	t.Helper()
	f := newPlanGroupFixture(t, base)
	confirmTestCredentialConfiguration(t, base.h, f.old.NodeID)
	confirmTestCredentialConfiguration(t, base.h, f.shared.NodeID)
	var before model.Subscription
	var plan model.Plan
	var sharedBefore model.ProtocolCredential
	var oldOrder model.Order
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", before.ID, f.shared.ID).First(&sharedBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&oldOrder, f.root.ID).Error; err != nil {
		t.Fatal(err)
	}
	var oldNode model.Node
	if err := f.db.First(&oldNode, f.old.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	_, sha, targets, err := base.h.compileNodeRuntimeConfigWithCredentialTargets(t.Context(), oldNode, "test-connector", "", false)
	if err != nil {
		t.Fatal(err)
	}
	state := base.h.services.ConfigurationPublicationState()
	started, err := state.Begin(t.Context(), network.ConfigurationPublicationRequest{NodeID: oldNode.ID})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesiredWithTargets(t.Context(), started.Deployment, sha, targets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	var after model.Subscription
	if err := f.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	expected := before
	expected.NodeGroupID, expected.UpdatedAt = after.NodeGroupID, after.UpdatedAt
	if after.NodeGroupID != f.group.ID || !reflect.DeepEqual(expected, after) {
		t.Fatalf("group change altered paid term/quota/identity %+v", after)
	}
	var orderAfter model.Order
	if err := f.db.First(&orderAfter, oldOrder.ID).Error; err != nil || !reflect.DeepEqual(oldOrder, orderAfter) {
		t.Fatal("group change rewrote historical payment", err)
	}
	var removed, sharedAfter model.ProtocolCredential
	if err := f.db.First(&removed, f.oldCredential.ID).Error; err != nil || removed.Status != "revoked" || removed.RevokedAt == nil {
		t.Fatal("removed endpoint kept authorization", err)
	}
	if err := f.db.First(&sharedAfter, sharedBefore.ID).Error; err != nil || sharedAfter.Secret != sharedBefore.Secret || sharedAfter.Status != sharedBefore.Status || sharedAfter.RevokedAt != nil {
		t.Fatal("shared endpoint lost its identity or authorization", err)
	}
	for _, node := range []uint{f.old.NodeID, f.shared.NodeID, f.added.NodeID} {
		var queue model.NodeConfigPublish
		if err := f.db.First(&queue, node).Error; err != nil {
			t.Fatalf("node %d has no recoverable group publication: %v", node, err)
		}
	}
	if err := base.h.services.NodeCredentialReconciliation(base.h.credentialCipher, true).ReconcileNode(t.Context(), f.added.NodeID); err != nil {
		t.Fatal(err)
	}
	var added model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", after.ID, f.added.ID).First(&added).Error; err != nil || !added.DeliveryRequired {
		t.Fatal("new endpoint was not prepared for explicit delivery", err)
	}
	statuses, err := credentialstore.DeliveryStatuses(f.db, []model.ProtocolCredential{sharedAfter, added})
	if err != nil || statuses[sharedAfter.ID] != "confirmed" || statuses[added.ID] != "pending" {
		t.Fatalf("old confirmation authorized a new endpoint %+v %v", statuses, err)
	}
	if err := base.h.services.CredentialPreparation(base.h.credentialCipher, true).Ensure(t.Context(), []entitlements.CredentialSubscription{{ID: before.ID, UserID: before.UserID, NodeGroupID: before.NodeGroupID, EndAt: before.EndAt}}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&removed, removed.ID).Error; err != nil || removed.Status != "revoked" {
		t.Fatal("old group reconciliation snapshot restored removed endpoint", err)
	}
	// This is a frozen-result persistence simulation, not native acceptance.
	if _, err := state.Complete(t.Context(), frozen, network.ConfigurationPublicationCompletion{ConfigSHA: sha, LastHealthyAt: time.Now().UTC(), ManagedPrincipalAccess: true, MieruAccess: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&removed, removed.ID).Error; err != nil || removed.Status != "revoked" || removed.RevokedAt == nil {
		t.Fatal("late old receipt restored revoked access", err)
	}
	closed := 0
	forbidden, err := (networkstore.ConnectionRevocation{DB: f.db, Cipher: base.h.credentialCipher}).WithRevokedPrincipal(t.Context(), removed.NodeID, removed.PrincipalKey, func() error { closed++; return nil })
	if err != nil || !forbidden || closed != 1 {
		t.Fatalf("current membership did not revoke removed scope: forbidden=%t calls=%d err=%v", forbidden, closed, err)
	}
	forbidden, err = (networkstore.ConnectionRevocation{DB: f.db, Cipher: base.h.credentialCipher}).WithRevokedPrincipal(t.Context(), sharedAfter.NodeID, sharedAfter.PrincipalKey, func() error { closed++; return nil })
	if err != nil || forbidden || closed != 1 {
		t.Fatalf("shared scope was closed: forbidden=%t calls=%d err=%v", forbidden, closed, err)
	}
	// Confirm the empty old-node configuration, then return A -> B -> A. The
	// previous receipt cannot prove the credential was reintroduced afterward.
	_, revokeSHA, revokeTargets, err := base.h.compileNodeRuntimeConfigWithCredentialTargets(t.Context(), oldNode, "test-connector", "", false)
	if err != nil || len(revokeTargets.Credentials) != 0 {
		t.Fatalf("removed source still compiled an old credential: %v", err)
	}
	revocation, err := state.Begin(t.Context(), network.ConfigurationPublicationRequest{NodeID: oldNode.ID})
	if err != nil {
		t.Fatal(err)
	}
	revocationFrozen, err := state.SetDesiredWithTargets(t.Context(), revocation.Deployment, revokeSHA, revokeTargets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(t.Context(), revocationFrozen, network.ConfigurationPublicationCompletion{ConfigSHA: revokeSHA, LastHealthyAt: time.Now().UTC(), ManagedPrincipalAccess: true, MieruAccess: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &base.group.ID, ExpectedRevision: &plan.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := base.h.services.NodeCredentialReconciliation(base.h.credentialCipher, true).ReconcileNode(t.Context(), oldNode.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&removed, removed.ID).Error; err != nil || removed.Status != "active" || removed.Secret != f.oldCredential.Secret {
		t.Fatal("current authorized return lost stable credential", err)
	}
	statuses, err = credentialstore.DeliveryStatuses(f.db, []model.ProtocolCredential{removed})
	if err != nil || statuses[removed.ID] != "pending" {
		t.Fatalf("old delivery was reused after confirmed revocation %+v %v", statuses, err)
	}
	if _, err := state.Complete(t.Context(), frozen, network.ConfigurationPublicationCompletion{ConfigSHA: sha, LastHealthyAt: time.Now().UTC(), ManagedPrincipalAccess: true, MieruAccess: true}); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("old configuration receipt retained ownership after newer revocation: %v", err)
	}
	statuses, err = credentialstore.DeliveryStatuses(f.db, []model.ProtocolCredential{removed})
	if err != nil || statuses[removed.ID] != "pending" {
		t.Fatalf("old receipt replay confirmed returned binding %+v %v", statuses, err)
	}
}

func TestPlanGroupSyncPublicationFailureRollsBackEverything(t *testing.T) {
	assertPlanGroupSyncRollback(t, newOrderFixture(t))
}
func TestMySQLPlanGroupSyncPublicationFailureRollsBackEverything(t *testing.T) {
	assertPlanGroupSyncRollback(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanGroupSyncRollback(t *testing.T, base orderFixture) {
	t.Helper()
	f := newPlanGroupFixture(t, base)
	var before model.Subscription
	var plan model.Plan
	if err := f.db.First(&before, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&plan, before.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	var queuesBefore []model.NodeConfigPublish
	if err := f.db.Order("node_id").Find(&queuesBefore).Error; err != nil {
		t.Fatal(err)
	}
	const hook = "test:plan-group-sync-audit-failure"
	if err := f.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*model.AuditLog); ok && row.Action == "subscription.plan_group.sync" {
			tx.AddError(errors.New("injected group intent failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, ExpectedRevision: &plan.Revision})
	f.db.Callback().Create().Remove(hook)
	if err == nil {
		t.Fatal("group transaction ignored durable intent failure")
	}
	var after model.Subscription
	var afterPlan model.Plan
	var credential model.ProtocolCredential
	var queuesAfter []model.NodeConfigPublish
	if err := f.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&afterPlan, plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&credential, f.oldCredential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("node_id").Find(&queuesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || afterPlan.NodeGroupID != plan.NodeGroupID || afterPlan.Revision != plan.Revision || credential.Status != "active" || credential.RevokedAt != nil || !reflect.DeepEqual(queuesBefore, queuesAfter) {
		t.Fatal("failed group change partially committed authorization, revision or delivery")
	}
}

func TestPlanGroupSyncPendingOrdersUseCurrentGroup(t *testing.T) {
	assertPlanGroupPendingOrders(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLPlanGroupSyncPendingOrdersUseCurrentGroup(t *testing.T) {
	assertPlanGroupPendingOrders(t, newMySQLBusinessOrderFixture)
}
func assertPlanGroupPendingOrders(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, operation := range []string{"new", "renewal", "upgrade"} {
		t.Run(operation, func(t *testing.T) {
			base := fixture(t)
			f := newPlanGroupFixture(t, base)
			request := commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "pending-before-plan-group-" + operation}
			if operation != "new" {
				request.TargetSubscriptionID = f.subscription
			}
			if operation == "upgrade" {
				request.PlanSKUID = f.target
			}
			preview, err := f.orders.Preview(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			request.QuoteFingerprint = preview.QuoteFingerprint
			created, err := f.orders.Create(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.payments.Configure(t.Context(), 1, created.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			var before model.Order
			var plan model.Plan
			if err := f.db.First(&before, created.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&plan, before.PlanID).Error; err != nil {
				t.Fatal(err)
			}
			if operation == "upgrade" {
				base.sku(t, plan.ID, 2000, "purchase")
			}
			if _, err := base.h.services.PlanUpdate.Update(t.Context(), 1, plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, ExpectedRevision: &plan.Revision}); err != nil {
				t.Fatalf("pending %s plan update: %#v", operation, err)
			}
			if _, err := f.payments.Pay(t.Context(), 1, created.ID); err != nil {
				t.Fatal(err)
			}
			var paid model.Order
			if err := f.db.First(&paid, created.ID).Error; err != nil {
				t.Fatal(err)
			}
			if paid.PurchaseSnapshot != before.PurchaseSnapshot || paid.ChangeSnapshot != before.ChangeSnapshot || paid.PayableAmount != before.PayableAmount || paid.PaidAmount != before.PayableAmount {
				t.Fatal("group change rewrote the old payment agreement")
			}
			var sub model.Subscription
			if err := f.db.First(&sub, paid.SubscriptionID).Error; err != nil || sub.NodeGroupID != f.group.ID {
				t.Fatalf("old pending %s restored old group %+v %v", operation, sub, err)
			}
			var outside int64
			if err := f.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND protocol_endpoint_id = ? AND status IN ? AND revoked_at IS NULL", sub.ID, f.old.ID, []string{"active", "prepared"}).Count(&outside).Error; err != nil || outside != 0 {
				t.Fatal("old pending payment restored removed endpoint", err)
			}
			var added model.ProtocolCredential
			if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", sub.ID, f.added.ID).First(&added).Error; err != nil || !added.DeliveryRequired {
				t.Fatal("current group had no prepared new credential", err)
			}
			var queue model.NodeConfigPublish
			if err := f.db.First(&queue, f.added.NodeID).Error; err != nil {
				t.Fatal("paid old order lacks durable new-group publication", err)
			}
		})
	}
}

func TestPlanGroupSyncChangesRecoverableScopeWithoutRevivingTerminalSubscriptions(t *testing.T) {
	assertPlanGroupLifecycleScope(t, newOrderFixture(t))
}
func TestMySQLPlanGroupSyncChangesRecoverableScopeWithoutRevivingTerminalSubscriptions(t *testing.T) {
	assertPlanGroupLifecycleScope(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanGroupLifecycleScope(t *testing.T, base orderFixture) {
	t.Helper()
	endpoint := planGroupEndpoint(t, base.h.db, "lifecycle-group")
	group := model.NodeGroup{Name: "Lifecycle scope", Code: "lifecycle-scope", IsEnabled: true}
	if err := base.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := base.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	yesterday, old := now.Add(-24*time.Hour), now.Add(-14*24*time.Hour)
	rows := []model.Subscription{
		{Lifecycle: "renewable", Status: "active", EndAt: now.Add(24 * time.Hour), FlowUsed: 100},
		{Lifecycle: "renewable", Status: "expired", EndAt: yesterday, EndedAt: &yesterday, EndReason: "expired"},
		{Lifecycle: "fixed", EndsOnQuotaExhaustion: true, Status: "active", EndAt: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), FlowUsed: 20},
		{Lifecycle: "fixed", EndsOnQuotaExhaustion: true, Status: "expired", EndAt: now.Add(24 * time.Hour), FlowUsed: 100},
		{Lifecycle: "renewable", Status: "expired", EndAt: now.Add(24 * time.Hour), EndedAt: &yesterday, EndReason: "canceled"},
		{Lifecycle: "renewable", Status: "expired", EndAt: old, EndedAt: &old, EndReason: "expired"},
		{Lifecycle: "renewable", Status: "ended", EndAt: now.Add(24 * time.Hour), EndedAt: &yesterday, EndReason: "expired"},
	}
	for i := range rows {
		rows[i].UserID, rows[i].PlanID, rows[i].PlanSKUID, rows[i].NodeGroupID = 1, base.planRecord.ID, base.skuRecord.ID, base.group.ID
		rows[i].StartAt, rows[i].FlowTotal, rows[i].ResetQuotaBytes, rows[i].Config = now.Add(-30*24*time.Hour), 100, 100, "{}"
		if err := base.h.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before []model.Subscription
	if err := base.h.db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := base.h.services.PlanUpdate.Update(t.Context(), 1, base.planRecord.ID, commerce.PlanUpdateRequest{NodeGroupID: &group.ID, ExpectedRevision: &base.planRecord.Revision}); err != nil {
		t.Fatal(err)
	}
	for i, expected := range before {
		var actual model.Subscription
		if err := base.h.db.First(&actual, expected.ID).Error; err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			expected.NodeGroupID = group.ID
		}
		expected.UpdatedAt = actual.UpdatedAt
		if !reflect.DeepEqual(expected, actual) {
			t.Fatalf("scope change revived/repriced lifecycle case %d: expected=%+v actual=%+v", i, expected, actual)
		}
		var credentials int64
		if err := base.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", actual.ID).Count(&credentials).Error; err != nil || credentials != 0 {
			t.Fatal("scope update claimed delivery or issued a terminal credential", err)
		}
	}
}

// A plan update can wait after a consistent RR snapshot has already formed.
// Revocation must use current membership and must not transiently revoke a
// confirmed shared credential in the hope a later Ensure will restore it.
func TestMySQLPlanGroupSyncKeepsConfirmedSharedMembershipAddedAfterSnapshot(t *testing.T) {
	base := newMySQLBusinessOrderFixture(t)
	f := newPlanGroupFixture(t, base)
	confirmTestCredentialConfiguration(t, base.h, f.shared.NodeID)
	var sharedBefore model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", f.subscription, f.shared.ID).First(&sharedBefore).Error; err != nil {
		t.Fatal(err)
	}
	var confirmationBefore model.CredentialDeliveryConfirmation
	if err := f.db.Where("credential_id = ?", sharedBefore.ID).First(&confirmationBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("node_group_id = ? AND protocol_endpoint_id = ?", f.group.ID, f.shared.ID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	transaction := f.db.Begin()
	if transaction.Error != nil {
		t.Fatal(transaction.Error)
	}
	defer transaction.Rollback()
	var observed int64
	if err := transaction.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ? AND protocol_endpoint_id = ?", f.group.ID, f.shared.ID).Count(&observed).Error; err != nil || observed != 0 {
		t.Fatal("failed to establish old missing shared membership snapshot", err)
	}
	if err := f.db.Create(&model.NodeGroupEndpoint{NodeGroupID: f.group.ID, ProtocolEndpointID: f.shared.ID}).Error; err != nil {
		t.Fatal(err)
	}
	updater := commerce.PlanUpdate{Repository: commercestore.PlanUpdate{DB: transaction}}
	if _, err := updater.Update(t.Context(), 1, base.planRecord.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, ExpectedRevision: &base.planRecord.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit().Error; err != nil {
		t.Fatal(err)
	}
	var sharedAfter model.ProtocolCredential
	if err := f.db.First(&sharedAfter, sharedBefore.ID).Error; err != nil || sharedAfter.Status != "active" || sharedAfter.RevokedAt != nil || sharedAfter.Secret != sharedBefore.Secret {
		t.Fatalf("old RR membership revoked confirmed shared scope: %+v %v", sharedAfter, err)
	}
	var confirmationAfter model.CredentialDeliveryConfirmation
	if err := f.db.Where("credential_id = ?", sharedBefore.ID).First(&confirmationAfter).Error; err != nil || !reflect.DeepEqual(confirmationBefore, confirmationAfter) {
		t.Fatal("scope sync rewrote confirmed delivery history", err)
	}
	statuses, err := credentialstore.DeliveryStatuses(f.db, []model.ProtocolCredential{sharedAfter})
	if err != nil || statuses[sharedAfter.ID] != "confirmed" {
		t.Fatal("current shared scope lost verified delivery", statuses, err)
	}
	var removed model.ProtocolCredential
	if err := f.db.First(&removed, f.oldCredential.ID).Error; err != nil || removed.Status != "revoked" {
		t.Fatal("old RR scope retained removed endpoint", err)
	}
}
