package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSubscriptionDeliveryOrderInterleavesEntriesAndLandingWithoutPublishing(t *testing.T) {
	f, a, endpoint := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, endpoint, "")
	other := model.ProtocolEndpoint{NodeID: endpoint.NodeID, Name: "other", Protocol: "trojan", Address: "other.test", Port: 444, IsActive: true}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	second := model.NetworkEntry{Name: "second entry", NodeID: a.ID, EndpointID: endpoint.ID, Address: a.Address, Port: 23457, PublicPort: 23457, Enabled: true, Network: "tcp"}
	if err := f.h.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadSubscriptionDeliveryOrder(f.h.db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 4 {
		t.Fatal(snapshot)
	}
	keys := []string{deliveryOrderKey(endpoint.ID, second.ID), deliveryOrderKey(other.ID, 0), deliveryOrderKey(endpoint.ID, entry.ID), deliveryOrderKey(endpoint.ID, 0)}
	var before int64
	f.h.db.Model(&model.NodeConfigPublish{}).Count(&before)
	request := func(keys []string, version string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"ordered_keys": keys, "expected_version": version})
		w := httptest.NewRecorder()
		f.h.SubscriptionDeliveryOrderHandler(w, announcementRequest(http.MethodPut, "/api/v1/admin/subscription-delivery-order", f.admin, string(raw)))
		return w
	}
	for _, invalid := range [][]string{keys[:3], {keys[0], keys[0], keys[2], keys[3]}, {"entry:999", keys[1], keys[2], keys[3]}} {
		if w := request(invalid, snapshot.Version); w.Code != 400 {
			t.Fatalf("invalid scope: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(keys, snapshot.Version); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	after, err := loadSubscriptionDeliveryOrder(f.h.db, false)
	if err != nil {
		t.Fatal(err)
	}
	actual := []string{}
	for _, item := range after.Items {
		actual = append(actual, item.Key)
	}
	if !reflect.DeepEqual(actual, keys) {
		t.Fatalf("order: %v", actual)
	}
	if w := request(keys, snapshot.Version); w.Code != 409 {
		t.Fatalf("stale version: %d", w.Code)
	}
	var publishes int64
	f.h.db.Model(&model.NodeConfigPublish{}).Count(&publishes)
	if publishes != before {
		t.Fatal("display ordering queued a runtime publish")
	}
	group := model.NodeGroup{Name: "ordered", Code: "ordered"}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{endpoint.ID, other.ID} {
		if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uint{entry.ID, second.ID} {
		if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	nodes := []subscriptionManifestNode{{ID: endpoint.ID}, {ID: endpoint.ID, NetworkEntryID: entry.ID}, {ID: other.ID}, {ID: endpoint.ID, NetworkEntryID: second.ID}}
	if err := f.h.sortSubscriptionManifestNodes([]model.Subscription{{ID: 1, NodeGroupID: group.ID}}, nodes); err != nil {
		t.Fatal(err)
	}
	actual = nil
	for _, n := range nodes {
		actual = append(actual, deliveryOrderKey(n.ID, n.NetworkEntryID))
	}
	if !reflect.DeepEqual(actual, keys) {
		t.Fatalf("subscription differs from editor: %v", actual)
	}
	// A normal edit must retain the separate delivery position.
	entry.Name = "renamed"
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"name":"renamed","node_id":%d,"endpoint_id":%d,"address":"entry.example.test","port":12345,"public_port":23456,"enabled":true,"revision":%d}`, a.ID, endpoint.ID, entry.Revision)
	f.h.NetworkEntriesHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/network-entries/%d", entry.ID), f.admin, body))
	if w.Code != 200 {
		t.Fatalf("edit: %s", w.Body.String())
	}
	var saved model.NetworkEntry
	f.h.db.First(&saved, entry.ID)
	if saved.DeliverySortOrder == nil || *saved.DeliverySortOrder != 2 {
		t.Fatal("entry edit reset order")
	}
}

func TestNetworkEntryNameOverridesLandingAndPreservesIdentity(t *testing.T) {
	base := subscriptionManifestNode{ID: 7, NodeID: 3, Name: "original landing", Protocol: "trojan", Address: "landing.example.test", Port: 443, CredentialID: "credential", Config: json.RawMessage(`{"type":"trojan","password":"secret"}`)}
	for _, test := range []struct{ name, want string }{
		{"广州专线", "广州专线"},
		{"  自定义入口  ", "自定义入口"},
		{"", "original landing"},
	} {
		front, err := projectNetworkEntry(base, model.NetworkEntry{ID: 9, Name: test.name, Address: "entry.example.test", Port: 1234, PublicPort: 1234})
		if err != nil {
			t.Fatal(err)
		}
		if front.Name != test.want || front.ID != base.ID || front.CredentialID != base.CredentialID {
			t.Fatalf("projection: %+v", front)
		}
		var cfg map[string]any
		json.Unmarshal(front.Config, &cfg)
		if cfg["sni"] != base.Address || cfg["password"] != "secret" {
			t.Fatal("landing identity changed")
		}
	}
}
