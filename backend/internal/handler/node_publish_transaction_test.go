package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func attachOrderPublishEndpoint(t testing.TB, f orderFixture) model.ProtocolEndpoint {
	t.Helper()
	node := model.Node{Name: "order-publish", Config: "{}"}
	if err := f.h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "vless-publish", Protocol: "vless", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 1000}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: f.group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	return endpoint
}
func rejectPublishInsert(t *testing.T, f orderFixture) {
	t.Helper()
	if err := f.h.db.Exec(`CREATE TRIGGER reject_publish BEFORE INSERT ON node_config_publishes
 BEGIN SELECT RAISE(ABORT, 'publication persistence unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
}

func TestOrderSettlementAndPublicationCommitTogether(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	order := f.create(t, 0)
	rejectPublishInsert(t, f)
	if w := f.pay(t, order.ID, false); w.Code != http.StatusInternalServerError {
		t.Fatalf("queue failure: %d %s", w.Code, w.Body.String())
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != orderStatusPending || actual.SubscriptionID != 0 {
		t.Fatal("order committed without publication")
	}
	for _, table := range []string{"subscriptions", "protocol_credentials", "quota_events", "audit_logs", "node_config_publishes"} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, order.ID)
	var queued model.NodeConfigPublish
	if err := f.h.db.First(&queued, endpoint.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if queued.EndpointID != endpoint.ID || queued.Generation != 1 || queued.LeaseToken != "" {
		t.Fatalf("publication wasn't committed pending: %+v", queued)
	}
	if w := f.pay(t, paid.ID, true); w.Code != http.StatusOK {
		t.Fatalf("repeat confirmation: %d", w.Code)
	}
	var repeated model.NodeConfigPublish
	if err := f.h.db.First(&repeated, endpoint.NodeID).Error; err != nil || repeated.Generation != queued.Generation {
		t.Fatal("repeat confirmation enqueued another generation")
	}
}

func TestCredentialExpiryRollsBackWhenPublicationCannotPersist(t *testing.T) {
	f := newOrderFixture(t)
	attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	rejectPublishInsert(t, f)
	if _, err := expireDueSubscriptionCredentials(f.h.db, time.Now().UTC(), 200); err == nil {
		t.Fatal("expiry ignored publication failure")
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.Status != subStatusActive {
		t.Fatal("expiry committed without durable revocation")
	}
	var credentials []model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", sub.ID).Find(&credentials).Error; err != nil || len(credentials) == 0 {
		t.Fatalf("credentials missing: %v", err)
	}
	for _, credential := range credentials {
		if credential.Status == "expired" {
			t.Fatal("credential expired outside transaction")
		}
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := expireDueSubscriptionCredentials(f.h.db, time.Now().UTC(), 200); err != nil {
		t.Fatal(err)
	}
	var pending model.NodeConfigPublish
	if err := f.h.db.First(&pending).Error; err != nil || pending.Generation != 2 {
		t.Fatalf("expiry publication missing: %+v %v", pending, err)
	}
}

func TestKernelCompletionAndReadinessPublicationCommitTogether(t *testing.T) {
	f := newOrderFixture(t)
	endpoint := attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&endpoint).Update("protocol", "mieru").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.ensureKernelState(endpoint.NodeID); err != nil {
		t.Fatal(err)
	}
	operation := model.NodeOperation{NodeID: endpoint.NodeID, OperationType: "upgrade", Status: "running", Phase: "verifying", RequestedBy: 1}
	if err := f.h.db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	rejectPublishInsert(t, f)
	probe := kernelProbe{Installed: true, Version: "0.0.15"}
	release := zeroRelease{Version: "0.0.15"}
	if _, err := f.h.finishKernelOperation(&operation, probe, release, "binary", "config", "verified"); err == nil {
		t.Fatal("kernel completion ignored queue failure")
	}
	var stored model.NodeOperation
	if err := f.h.db.First(&stored, operation.ID).Error; err != nil || stored.Status != "running" {
		t.Fatalf("kernel completion partially committed: %+v %v", stored, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_publish").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.finishKernelOperation(&operation, probe, release, "binary", "config", "verified"); err != nil {
		t.Fatal(err)
	}
	var pending model.NodeConfigPublish
	if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil || pending.EndpointID != endpoint.ID {
		t.Fatalf("readiness publication missing: %+v %v", pending, err)
	}
}
