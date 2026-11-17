package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAccountProtocolLoadKeepsAuthorizedProtocolWhenHeartbeatIsStale(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	node := model.Node{Name: "delayed-node", Config: "{}", IsEnabled: true, LastSeenAt: &old, ConnectorLastSeenAt: &old, Version: "0.0.3-dev.202610162206"}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "authorized-protocol", RuntimeKey: "authorized", Protocol: "shadowsocks", Address: "private.example", Port: 21388, IsActive: true}
	if err := h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "entitled", Code: "entitled"}
	if err := h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	membership := model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}
	if err := h.db.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
	subscription := model.Subscription{UserID: 1, NodeGroupID: group.ID, Status: "active", EndAt: now.Add(time.Hour), FlowTotal: 100}
	if err := h.db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	observation := principalFlowCurrent{NodeID: node.ID, PrincipalKey: "principal", CoreInstanceID: "core", UserID: 1, SubscriptionID: subscription.ID, ProtocolEndpointID: endpoint.ID, ActiveFlows: 9, ObservedAt: old, UpdatedAt: old}
	if err := h.db.Create(&observation).Error; err != nil {
		t.Fatal(err)
	}
	read := func() accountProtocolLoadResponse {
		t.Helper()
		response := httptest.NewRecorder()
		h.AccountProtocolLoadHandler(response, announcementRequest(http.MethodGet, "/api/v1/subscription/protocol-loads", token, ""))
		if response.Code != 200 {
			t.Fatalf("response %d: %s", response.Code, response.Body.String())
		}
		var body struct {
			Data accountProtocolLoadResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Data
	}
	got := read()
	if len(got.Items) != 1 {
		t.Fatalf("stale heartbeat hid authorized protocol: %+v", got)
	}
	item := got.Items[0]
	if item.ConnectorOnline || item.LoadFresh || item.ActiveFlows != 0 || item.LoadObservedAt == nil {
		t.Fatalf("stale observations treated as live: %+v", item)
	}
	if err := h.db.Model(&node).Update("connector_last_seen_at", now).Error; err != nil {
		t.Fatal(err)
	}
	got = read()
	if !got.Items[0].ConnectorOnline || got.Items[0].LoadFresh {
		t.Fatalf("receipt freshness confused with load freshness: %+v", got.Items[0])
	}
	if err := h.db.Model(&observation).Updates(map[string]interface{}{"observed_at": now, "active_flows": 0}).Error; err != nil {
		t.Fatal(err)
	}
	got = read()
	if !got.Items[0].LoadFresh || got.Items[0].ActiveFlows != 0 {
		t.Fatalf("fresh authoritative zero lost: %+v", got.Items[0])
	}
	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"other user", map[string]interface{}{"user_id": 2}},
		{"expired", map[string]interface{}{"user_id": 1, "end_at": old}},
		{"exhausted", map[string]interface{}{"end_at": now.Add(time.Hour), "flow_used": 100}},
		{"inactive", map[string]interface{}{"flow_used": 0, "status": "cancelled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := h.db.Model(&subscription).Updates(tc.updates).Error; err != nil {
				t.Fatal(err)
			}
			if got := read(); len(got.Items) != 0 {
				t.Fatalf("unauthorized protocol exposed: %+v", got)
			}
		})
	}
	if err := h.db.Model(&subscription).Updates(map[string]interface{}{"status": "active", "flow_used": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&node).Update("is_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got.Items) != 0 {
		t.Fatalf("disabled node exposed: %+v", got)
	}
}
