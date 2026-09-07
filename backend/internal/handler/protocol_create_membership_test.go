package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCreateProtocolWithNodeGroupMembership(t *testing.T) {
	f := newTrafficReadFixture(t)
	node := model.Node{Name: "membership-node", Address: "192.0.2.1", Config: "{}"}
	group := model.NodeGroup{Name: "group", Code: "group", IsEnabled: true, Revision: 1}
	for _, record := range []interface{}{&node, &group} {
		if err := f.h.db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Use the exact lookup response the editor receives, including its revision.
	var page struct {
		Items []nodeGroupSummaryItem `json:"items"`
	}
	f.get(t, "/api/v1/admin/node-groups?paged=true&offset=0&limit=25", true, f.h.NodeGroupListHandler, &page)
	if len(page.Items) != 1 {
		t.Fatalf("lookup=%+v", page)
	}
	payload := map[string]interface{}{"node_id": node.ID, "name": "Created with group", "protocol": "vless", "address": node.Address, "port": 1443, "public_port": 1443, "multiplier_milli": 1000, "is_active": true, "config": `{"type":"vless","users":[]}`, "client_config": fmt.Sprintf(`{"type":"vless","server":%q,"port":1443}`, node.Address), "optional_config": "{}", "tags": "[]", "node_group_membership_changes": []protocolEndpointNodeGroupMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: page.Items[0].Revision, Member: true}}}
	body, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	f.h.ProtocolEndpointCreateHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/protocol-endpoints", f.admin, string(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var links []model.NodeGroupEndpoint
	if err := f.h.db.Where("node_group_id = ?", group.ID).Find(&links).Error; err != nil || len(links) != 1 {
		t.Fatalf("links=%+v err=%v", links, err)
	}
	var response struct {
		Data protocolEndpointMutationResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	result := response.Data
	if result.ProtocolEndpoint.ID == 0 || links[0].ProtocolEndpointID != result.ProtocolEndpoint.ID {
		t.Fatalf("link does not point to created protocol: %+v %+v", links, result)
	}
	if len(result.NodeGroupMemberships) != 1 || result.NodeGroupMemberships[0].NodeGroupID != group.ID {
		t.Fatalf("create response did not confirm binding: %+v", result)
	}
	if result.NodeGroupMembership == nil || len(result.NodeGroupMembership.AddedNodeGroupIDs) != 1 || result.NodeGroupMembership.AddedNodeGroupIDs[0] != group.ID {
		t.Fatalf("missing binding result: %+v", result)
	}
	var detail protocolEndpointAdminDetail
	f.get(t, fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", result.ProtocolEndpoint.ID), true, f.h.ProtocolEndpointDetailHandler, &detail)
	if len(detail.NodeGroupMemberships) != 1 || detail.NodeGroupMemberships[0].NodeGroupID != group.ID {
		t.Fatalf("binding not retained after reload: %+v", detail.NodeGroupMemberships)
	}
	// Drain the local reconciliation task before closing the fixture database.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var running int64
		f.h.db.Model(&model.Task{}).Where("status IN ?", []int16{taskStatusPending, taskStatusRunning}).Count(&running)
		if running == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("membership reconciliation did not finish")
}

func TestCreateProtocolRollsBackWhenSelectedGroupCannotBeBound(t *testing.T) {
	for _, test := range []struct {
		name    string
		missing bool
		status  int
	}{
		{"group revision conflict", false, http.StatusConflict},
		{"selected group deleted", true, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newTrafficReadFixture(t)
			node := model.Node{Name: "membership-rollback-node", Address: "192.0.2.1", Config: "{}"}
			group := model.NodeGroup{Name: "group", Code: "group", IsEnabled: true, Revision: 2}
			for _, record := range []interface{}{&node, &group} {
				if err := f.h.db.Create(record).Error; err != nil {
					t.Fatal(err)
				}
			}
			groupID := group.ID
			if test.missing {
				groupID += 1000
			}
			payload := map[string]interface{}{
				"node_id": node.ID, "name": "Must not be orphaned", "protocol": "vless", "address": node.Address,
				"port": 1443, "public_port": 1443, "multiplier_milli": 1000, "is_active": true,
				"config": `{"type":"vless","users":[]}`, "client_config": `{"type":"vless","server":"192.0.2.1","port":1443}`,
				"optional_config": "{}", "tags": "[]",
				"node_group_membership_changes": []protocolEndpointNodeGroupMembershipChange{{NodeGroupID: groupID, ExpectedRevision: 1, Member: true}},
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			f.h.ProtocolEndpointCreateHandler(response, announcementRequest(http.MethodPost, "/api/v1/admin/protocol-endpoints", f.admin, string(body)))
			if response.Code != test.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			for _, record := range []interface{}{&model.ProtocolEndpoint{}, &model.NodeGroupEndpoint{}, &model.NodeConfigPublish{}, &model.Task{}} {
				var count int64
				if err := f.h.db.Model(record).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("failed binding left %d rows in %T", count, record)
				}
			}
		})
	}
}
