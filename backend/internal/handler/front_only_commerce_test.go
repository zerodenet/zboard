package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestFrontOnlyProductPaymentDeliversParentCredentialWithoutDirectRoute(t *testing.T) {
	f := newOrderFixture(t)
	parent := attachOrderPublishEndpoint(t, f)
	parent.Address = "landing.example.test"
	parent.ClientConfig = `{"type":"vless","server":"landing.example.test","port":443}`
	parent.ServerConfig, _ = f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err := f.h.db.Save(&parent).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := f.h.db.Model(&model.Node{}).Where("id = ?", parent.NodeID).Updates(map[string]any{"is_enabled": true, "last_seen_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/network-entries", f.token, fmt.Sprintf(`{"deployment_mode":"external","name":"Front only","parent_protocol_id":%d,"address":"front.example.test","port":8443,"enabled":true}`, parent.ID)))
	if w.Code != 200 {
		t.Fatalf("create entry: %s", w.Body.String())
	}
	var entryResponse struct{ Data model.NetworkEntry }
	if err := json.Unmarshal(w.Body.Bytes(), &entryResponse); err != nil {
		t.Fatal(err)
	}
	var group model.NodeGroup
	if err := f.h.db.First(&group, f.group.ID).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	f.h.NodeGroupUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/node-groups/%d", group.ID), f.token, fmt.Sprintf(`{"expected_revision":%d,"protocol_endpoint_ids":[],"network_entry_ids":[%d]}`, group.Revision, entryResponse.Data.ID)))
	if w.Code != 200 {
		t.Fatalf("grant front only: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	f.h.PlanUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/plans/%d", f.planRecord.ID), f.token, fmt.Sprintf(`{"is_active":true,"expected_revision":%d}`, f.planRecord.Revision)))
	if w.Code != 200 {
		t.Fatalf("front-only product unavailable: %s", w.Body.String())
	}
	order := f.create(t, 0)
	var credentials int64
	if err := f.h.db.Model(&model.ProtocolCredential{}).Count(&credentials).Error; err != nil || credentials != 0 {
		t.Fatalf("unpaid order granted credentials: %d %v", credentials, err)
	}
	paid := f.paid(t, order.ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	var publication model.NodeConfigPublish
	if err := f.h.db.First(&publication, parent.NodeID).Error; err != nil {
		t.Fatal("parent publication missing", err)
	}
	// Simulate the publication being applied. This verifies the local paid
	// entitlement and delivery boundary, not a remote Zero connection.
	if err := f.h.db.Where("node_id = ?", parent.NodeID).Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err := f.h.buildProjectedSubscriptionManifestNodes(context.Background(), []model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 1 || nodes[0].Address != "front.example.test" || nodes[0].CredentialID == "" {
		t.Fatalf("paid delivery: %+v %v", nodes, err)
	}
	if len(activeEndpointCredentialsForTest(t, f.h, parent.ID, now)) != 1 {
		t.Fatal("parent runtime did not receive paid credential")
	}
	f.paid(t, order.ID)
	if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", sub.ID).Count(&credentials).Error; err != nil || credentials != 1 {
		t.Fatalf("duplicate payment duplicated credentials: %d %v", credentials, err)
	}
}
