package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolListenAddressPersistsPublishesAndSurvivesLegacyUpdate(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example"}).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "listen-node", Address: "192.0.2.1", Config: "{}"}
	if err := f.h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"node_id": node.ID, "name": "listener", "protocol": "vless", "address": "public.example",
		"listen_address": "[::]", "port": 1443, "public_port": 8443, "multiplier_milli": 1000, "is_active": true,
		"config": `{"type":"vless","users":[]}`, "client_config": `{"type":"vless","server":"public.example","port":8443}`,
		"optional_config": "{}", "tags": "[]",
	}
	save := func(id uint) protocolEndpointMutationResponse {
		t.Helper()
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		if id == 0 {
			f.h.ProtocolEndpointCreateHandler(w, announcementRequest("POST", "/api/v1/admin/protocol-endpoints", f.admin, string(body)))
		} else {
			f.h.ProtocolEndpointUpdateHandler(w, announcementRequest("PUT", fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", id), f.admin, string(body)))
		}
		if w.Code != http.StatusOK {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Data protocolEndpointMutationResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data
	}
	created := save(0)
	id := created.ProtocolEndpoint.ID
	if id == 0 || created.ProtocolEndpoint.ListenAddress != "::" || created.PublishStatus != "queued" {
		t.Fatalf("create lost listener: %+v", created)
	}
	var detail protocolEndpointAdminDetail
	f.get(t, fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", id), true, f.h.ProtocolEndpointDetailHandler, &detail)
	if detail.ListenAddress != "::" || detail.Address != "public.example" || detail.PublicPort != 8443 {
		t.Fatalf("readback changed delivery: %+v", detail.ProtocolEndpoint)
	}
	snapshot, err := f.h.services.RuntimeConfigurationSource().Load(context.Background(), node.ID, time.Now(), []string{"vless"})
	if err != nil || len(snapshot.Endpoints) != 1 || snapshot.Endpoints[0].ListenAddress != "::" {
		t.Fatalf("publication source lost listener: %+v %v", snapshot, err)
	}
	rendered, _, err := (zero.RuntimeConfigurationRenderer{Cipher: f.h.credentialCipher}).Render(zero.RuntimeConfigurationRenderRequest{
		NodeID: node.ID, APIKey: "fixture", NativeConnector: true, Now: time.Now(), Snapshot: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	var configuration struct {
		Inbounds []struct {
			Listen struct {
				Address string
				Port    int
			}
		}
	}
	if err := json.Unmarshal(rendered, &configuration); err != nil || len(configuration.Inbounds) != 1 || configuration.Inbounds[0].Listen.Address != "[::]" || configuration.Inbounds[0].Listen.Port != 1443 {
		t.Fatalf("published wrong listener: %s %v", rendered, err)
	}
	// Updating through an older client must not reset an explicitly selected IPv6 bind.
	delete(payload, "listen_address")
	payload["name"] = "renamed"
	updated := save(id)
	if updated.ProtocolEndpoint.ListenAddress != "::" || updated.PublishStatus != "not_required" {
		t.Fatalf("legacy update reset listener: %+v", updated)
	}
	payload["listen_address"] = "192.0.2.10"
	updated = save(id)
	if updated.Effect != "runtime" || updated.PublishStatus != "queued" || updated.ProtocolEndpoint.ListenAddress != "192.0.2.10" || updated.ProtocolEndpoint.Address != "public.example" || updated.ProtocolEndpoint.PublicPort != 8443 {
		t.Fatalf("listener update was not a runtime-only change: %+v", updated)
	}
	var publication model.NodeConfigPublish
	if err := f.h.db.First(&publication, "node_id = ?", node.ID).Error; err != nil || publication.Generation < 2 {
		t.Fatalf("listener update did not advance publication: %+v %v", publication, err)
	}
	// Invalid input must not persist or advance desired configuration.
	payload["listen_address"] = "[::]:443"
	body, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	f.h.ProtocolEndpointUpdateHandler(w, announcementRequest("PUT", fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", id), f.admin, string(body)))
	if w.Code != 400 {
		t.Fatalf("invalid listener accepted: %d %s", w.Code, w.Body.String())
	}
	var stored model.ProtocolEndpoint
	if err := f.h.db.First(&stored, id).Error; err != nil || stored.ListenAddress != "192.0.2.10" {
		t.Fatalf("invalid update persisted: %+v %v", stored, err)
	}
	var after model.NodeConfigPublish
	if err := f.h.db.First(&after, "node_id = ?", node.ID).Error; err != nil || after.Generation != publication.Generation {
		t.Fatalf("invalid input queued publication: %+v %v", after, err)
	}
}
