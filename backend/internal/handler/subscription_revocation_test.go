package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func clearPublishTestJobs(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Exec("DELETE FROM node_config_publishes").Error; err != nil {
		t.Fatal(err)
	}
}

func TestReadExpiryPersistsRevocationBeforeWorkerSkipsSubscription(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	clearPublishTestJobs(t, f)
	now := time.Now().UTC()
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", now.Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := expireSubscriptions(f.h.db, paid.UserID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := expireDueSubscriptionCredentials(f.h.db, now, 200); err != nil {
		t.Fatal(err)
	}
	var job model.NodeConfigPublish
	if err := f.h.db.First(&job, endpoint.NodeID).Error; err != nil {
		t.Fatalf("read-side expiry lost revocation: %v", err)
	}
	if job.Generation != 1 {
		t.Fatalf("duplicate expiry publication: %+v", job)
	}
}

func TestReadExpiryRollsBackWhenRevocationCannotPersist(t *testing.T) {
	f := newOrderFixture(t)
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	clearPublishTestJobs(t, f)
	now := time.Now().UTC()
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", now.Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	rejectPublishInsert(t, f)
	if err := expireSubscriptions(f.h.db, paid.UserID, now); err == nil {
		t.Fatal("expiry ignored queue failure")
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil || sub.Status != subStatusActive {
		t.Fatalf("expiry partially committed: %+v %v", sub, err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", sub.ID).First(&credential).Error; err != nil || credential.Status != protocolCredentialStatusActive {
		t.Fatalf("credential partially expired: %+v %v", credential, err)
	}
}

func prepareGroupChange(t *testing.T) (orderFixture, model.Order, model.ProtocolEndpoint, model.ProtocolEndpoint) {
	t.Helper()
	f := newOrderFixture(t)
	oldEndpoint := attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	clearPublishTestJobs(t, f)
	group := model.NodeGroup{Name: "Replacement", Code: "replacement", IsEnabled: true}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	f.group = group
	plan := f.plan(t, 2)
	sku := f.sku(t, plan.ID, 200, skuOperationChange)
	f.planRecord, f.skuRecord = plan, sku
	node := model.Node{Name: "replacement-node"}
	if err := f.h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "replacement-vless", RuntimeKey: "c1bd8316-c8b9-4d2c-b1dc-123baf639bc0", Protocol: "vless", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", MultiplierMilli: 1000}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	change := f.create(t, paid.SubscriptionID)
	if change.OrderType != "upgrade" {
		t.Fatalf("change type = %s", change.OrderType)
	}
	return f, change, oldEndpoint, endpoint
}

func TestPlanChangeRevokesOldGroupAndPublishesBothNodes(t *testing.T) {
	f, change, oldEndpoint, newEndpoint := prepareGroupChange(t)
	f.paid(t, change.ID)
	var old model.ProtocolCredential
	if err := f.h.db.Where("protocol_endpoint_id = ? AND subscription_id = ?", oldEndpoint.ID, *change.TargetSubscriptionID).First(&old).Error; err != nil {
		t.Fatal(err)
	}
	if old.Status != protocolCredentialStatusRevoked || old.RevokedAt == nil {
		t.Fatal("old-group credential remains active")
	}
	for _, endpoint := range []model.ProtocolEndpoint{oldEndpoint, newEndpoint} {
		var job model.NodeConfigPublish
		if err := f.h.db.First(&job, endpoint.NodeID).Error; err != nil {
			t.Fatalf("missing publication on node %d: %v", endpoint.NodeID, err)
		}
	}
	current, err := f.h.activeEndpointCredentials(newEndpoint.ID, time.Now().UTC())
	if err != nil || len(current) != 1 {
		t.Fatalf("new group entitlement unavailable: %v %v", current, err)
	}
	previous, err := f.h.activeEndpointCredentials(oldEndpoint.ID, time.Now().UTC())
	if err != nil || len(previous) != 0 {
		t.Fatalf("old node projection still grants access: %v %v", previous, err)
	}
}

func TestPlanChangeRevocationFailurePreservesOriginalEntitlement(t *testing.T) {
	f, change, oldEndpoint, _ := prepareGroupChange(t)
	rejectPublishInsert(t, f)
	if w := f.pay(t, change.ID, false); w.Code != 500 {
		t.Fatalf("revocation failure: %d %s", w.Code, w.Body.String())
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, *change.TargetSubscriptionID).Error; err != nil || sub.PlanID == change.PlanID {
		t.Fatalf("failed change replaced entitlement: %+v %v", sub, err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("protocol_endpoint_id = ? AND subscription_id = ?", oldEndpoint.ID, sub.ID).First(&credential).Error; err != nil || credential.Status != protocolCredentialStatusActive || credential.RevokedAt != nil {
		t.Fatalf("failed change revoked old access: %+v %v", credential, err)
	}
	var order model.Order
	if err := f.h.db.First(&order, change.ID).Error; err != nil || order.Status != orderStatusPending {
		t.Fatalf("failed change settled order: %+v %v", order, err)
	}
}

func TestPlanChangeRetainsEndpointSharedByBothGroups(t *testing.T) {
	f, change, oldEndpoint, _ := prepareGroupChange(t)
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: f.group.ID, ProtocolEndpointID: oldEndpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	var before model.ProtocolCredential
	if err := f.h.db.Where("protocol_endpoint_id = ? AND subscription_id = ?", oldEndpoint.ID, *change.TargetSubscriptionID).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	f.paid(t, change.ID)
	var after model.ProtocolCredential
	if err := f.h.db.First(&after, before.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != protocolCredentialStatusActive || after.RevokedAt != nil || after.CredentialID != before.CredentialID {
		t.Fatal("shared endpoint lost its existing credential")
	}
}
