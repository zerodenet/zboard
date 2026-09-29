package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExternalForwardEntryGrantDeliveryAndRemoval(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	verifyExternalForwardClosure(t, f, a, b)
}
func TestMySQLExternalForwardEntryGrantDeliveryAndRemoval(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	user := model.User{ID: 1, Email: "external-forward-admin@example.test", Password: "hash", Status: userStatusActive, IsAdmin: true}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	f, a, b := seedNetworkEntryFixture(t, trafficReadFixture{h: h, admin: token, token: token})
	verifyExternalForwardClosure(t, f, a, b)
}
func verifyExternalForwardClosure(t *testing.T, f trafficReadFixture, a model.Node, b model.ProtocolEndpoint) {
	t.Helper()
	b.Protocol = "vless"
	b.ClientConfig = `{"type":"vless","server":"landing.example.test","port":1443}`
	b.ServerConfig, _ = f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err := f.h.db.Save(&b).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/network-entries", f.admin, fmt.Sprintf(`{"deployment_mode":"external","name":"External","parent_protocol_id":%d,"address":"front.example.test","port":8443,"public_port":8443,"enabled":true}`, b.ID)))
	if w.Code != 200 {
		t.Fatalf("external save: %d %s", w.Code, w.Body.String())
	}
	var response struct{ Data model.NetworkEntry }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	entry := response.Data
	if entry.NodeID != 0 || entry.DeploymentMode != "external" {
		t.Fatalf("entry=%+v", entry)
	}
	var nullNode int64
	if err := f.h.db.Model(&model.NetworkEntry{}).Where("id = ? AND node_id IS NULL", entry.ID).Count(&nullNode).Error; err != nil || nullNode != 1 {
		t.Fatalf("external entry must not invent an A node: %v %d", err, nullNode)
	}
	var publications []model.NodeConfigPublish
	if err := f.h.db.Find(&publications).Error; err != nil {
		t.Fatal(err)
	}
	for _, publication := range publications {
		if publication.NodeID == a.ID || publication.NodeID == 0 {
			t.Fatalf("external entry deployed on A: %+v", publications)
		}
	}
	group := model.NodeGroup{Name: "External only", Code: "external-only", IsEnabled: true}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "External only", Slug: "external-only", NodeGroupID: group.ID, TrafficBytes: 1000}
	if err := f.h.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "Monthly", Code: "external-monthly", BillingUnit: "month", BillingValue: 1, PriceCents: 100, Currency: "CNY"}
	if err := f.h.db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sub := model.Subscription{UserID: 1, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: subStatusActive, StartAt: now, EndAt: now.Add(time.Hour), FlowTotal: 1000, Config: "{}"}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.ensureCredentialsForSubscriptions([]model.Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Where("1=1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err := f.h.buildProjectedSubscriptionManifestNodes(context.Background(), []model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 1 || nodes[0].Address != entry.Address || nodes[0].CredentialID == "" {
		t.Fatalf("entry-only manifest: %+v %v", nodes, err)
	}
	if got := activeEndpointCredentialsForTest(t, f.h, b.ID, now); len(got) != 1 {
		t.Fatalf("runtime credentials=%+v", got)
	}
	// A known asset remains unaffected; external registration emits no listener.
	payload, _, err := f.h.compileNodeRuntimeConfig(a, "fixture", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var runtime struct {
		Inbounds []struct {
			Tag string `json:"tag"`
		}
	}
	if err := json.Unmarshal(payload, &runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.Inbounds) != 1 || runtime.Inbounds[0].Tag != "zboard-control-bootstrap" {
		t.Fatalf("external entry created listener on A: %s", payload)
	}
	w = httptest.NewRecorder()
	f.h.ProtocolEndpointListHandler(w, announcementRequest(http.MethodGet, "/api/v1/admin/protocol-endpoints?paged=true&service_kind=all&limit=1&include_facets=true", f.admin, ""))
	if w.Code != 200 {
		t.Fatalf("combined page: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Data struct {
			Items []protocolEndpointListItem
			Total int64
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data.Total != 2 || len(page.Data.Items) != 1 {
		t.Fatalf("page=%s", w.Body.String())
	}
	w = httptest.NewRecorder()
	f.h.ProtocolEndpointListHandler(w, announcementRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/protocol-endpoints?paged=true&service_kind=forward&node_group_id=%d&limit=1", group.ID), f.admin, ""))
	if w.Code != 200 {
		t.Fatalf("forward filter: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data.Total != 1 || len(page.Data.Items) != 1 || page.Data.Items[0].ServiceKind != "forward" || page.Data.Items[0].ID != entry.ID || page.Data.Items[0].Forward == nil {
		t.Fatalf("typed forward/group page=%s", w.Body.String())
	}
	verifyForwardMembershipQueries(t, f, b, entry, group)
	// Editing an external entry must retain SQL NULL for the absent A node.
	w = httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), f.admin, fmt.Sprintf(`{"deployment_mode":"external","revision":%d,"name":"External edited","parent_protocol_id":%d,"address":"front.example.test","port":8443,"public_port":8443,"enabled":true}`, entry.Revision, b.ID)))
	if w.Code != 200 {
		t.Fatalf("external edit: %d %s", w.Code, w.Body.String())
	}
	if err := f.h.db.Model(&model.NetworkEntry{}).Where("id = ? AND node_id IS NULL", entry.ID).Count(&nullNode).Error; err != nil || nullNode != 1 {
		t.Fatalf("external edit invented an A node: %v %d", err, nullNode)
	}
	w = httptest.NewRecorder()
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), f.admin, ""))
	if w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	if got := activeEndpointCredentialsForTest(t, f.h, b.ID, now); len(got) != 0 {
		t.Fatalf("removed entry still authenticates: %+v", got)
	}
	if err := f.h.reconcileNodeGroupCredentials(group.ID); err != nil {
		t.Fatal(err)
	}
	var credential model.ProtocolCredential
	f.h.db.Where("subscription_id = ?", sub.ID).First(&credential)
	if credential.Status != protocolCredentialStatusRevoked {
		t.Fatalf("final grant not revoked: %+v", credential)
	}
}

func verifyForwardMembershipQueries(t *testing.T, f trafficReadFixture, parent model.ProtocolEndpoint, entry model.NetworkEntry, group model.NodeGroup) {
	t.Helper()
	other := model.NetworkEntry{DeploymentMode: "external", EndpointID: parent.ID, Name: "Unassigned entry", Address: "other.example.test", Port: 9443, PublicPort: 9443, Network: "tcp_udp", Enabled: true}
	if err := f.h.db.Omit("node_id").Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.ProtocolEndpointListHandler(w, announcementRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/protocol-endpoints?paged=true&service_kind=forward&ids=%d&limit=50", other.ID), f.admin, ""))
	var page struct {
		Data struct {
			Items []protocolEndpointListItem
			Total int64
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || page.Data.Total != 1 || len(page.Data.Items) != 1 || page.Data.Items[0].ID != other.ID {
		t.Fatalf("forward hydration mixed IDs: %s %v", w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	f.h.ProtocolEndpointSelectionHandler(w, announcementRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/protocol-endpoints/selection?service_kind=forward&node_group_id=%d&active=true", group.ID), f.admin, ""))
	var selected struct {
		Data protocolEndpointSelectionSnapshot
	}
	if err := json.Unmarshal(w.Body.Bytes(), &selected); err != nil || w.Code != 200 || selected.Data.Total != 1 || len(selected.Data.IDs) != 1 || selected.Data.IDs[0] != entry.ID {
		t.Fatalf("forward selection mismatch: %s %v", w.Body.String(), err)
	}
	for _, handler := range []http.HandlerFunc{f.h.ProtocolEndpointListHandler, f.h.ProtocolEndpointSelectionHandler} {
		w = httptest.NewRecorder()
		handler(w, announcementRequest(http.MethodGet, "/api/v1/admin/protocol-endpoints?service_kind=all&ids=1", f.admin, ""))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("accepted ambiguous identity scope: %s", w.Body.String())
		}
	}
}
