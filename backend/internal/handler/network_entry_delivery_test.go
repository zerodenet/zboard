package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestNetworkEntriesNeverGrantLandingCredentialsImplicitly(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	b.Protocol = "vless"
	b.ClientConfig = `{"type":"vless","server":"landing.example.test","port":1443}`
	b.ServerConfig, _ = f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err := f.h.db.Save(&b).Error; err != nil {
		t.Fatal(err)
	}
	entry := saveEntryForTest(t, f, a, b, "")
	group := model.NodeGroup{Name: "entry only", Code: "entry-only"}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sub := model.Subscription{UserID: 1, NodeGroupID: group.ID, Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 1000}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.ensureCredentialsForSubscriptions([]model.Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	var count int64
	f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", sub.ID).Count(&count)
	if count != 0 {
		t.Fatal("entry implicitly created B credentials")
	}
	f.h.db.Where("1 = 1").Delete(&model.NodeConfigPublish{})
	nodes, err := f.h.buildProjectedSubscriptionManifestNodes([]model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("entry-only subscription delivered credentials: %v %v", nodes, err)
	}
	// Explicit B permission supplies one credential shared by direct and front.
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: b.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.ensureCredentialsForSubscriptions([]model.Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	f.h.db.Where("1 = 1").Delete(&model.NodeConfigPublish{})
	nodes, err = f.h.buildProjectedSubscriptionManifestNodes([]model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 2 || nodes[0].CredentialID == "" || nodes[0].CredentialID != nodes[1].CredentialID {
		t.Fatalf("explicit A+B: %v %v", nodes, err)
	}
	credentials, err := f.h.activeEndpointCredentials(b.ID, now)
	if err != nil || len(credentials) != 1 {
		t.Fatalf("explicit B auth: %v %v", credentials, err)
	}
	// Removing only B must reject both delivery and runtime auth immediately,
	// including still-active credential rows created before the removal.
	if err := f.h.db.Where("node_group_id = ?", group.ID).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err = f.h.buildProjectedSubscriptionManifestNodes([]model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("stale B credential leaked: %v %v", nodes, err)
	}
	credentials, err = f.h.activeEndpointCredentials(b.ID, now)
	if err != nil || len(credentials) != 0 {
		t.Fatalf("removed B grant still authorizes: %v %v", credentials, err)
	}
	if err := f.h.reconcileNodeGroupCredentials(group.ID); err != nil {
		t.Fatal(err)
	}
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", sub.ID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Status != protocolCredentialStatusRevoked {
		t.Fatal("old implicit credential was not revoked")
	}
	// Static templates are also forbidden from smuggling a landing credential.
	b.Protocol = "http"
	b.ClientConfig = `{"type":"http","username":"shared","password":"secret"}`
	f.h.db.Save(&b)
	nodes, err = f.h.buildAuthorizedNetworkEntries([]model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("static B config bypassed grant: %v %v", nodes, err)
	}
}

func TestNodeGroupCanAssignOnlyNetworkEntryAndRejectStaleOrInvalidUpdates(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	response := httptest.NewRecorder()
	f.h.NodeGroupCreateHandler(response, announcementRequest(http.MethodPost, "/api/v1/admin/node-groups", f.admin,
		fmt.Sprintf(`{"name":"Front only","code":"front-only","is_enabled":true,"protocol_endpoint_ids":[],"network_entry_ids":[%d]}`, entry.ID)))
	if response.Code != 200 {
		t.Fatalf("create front-only group: %d %s", response.Code, response.Body.String())
	}
	var created struct{ Data nodeGroupMutationResponse }
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	group := created.Data.NodeGroup
	if len(group.ProtocolEndpointIDs) != 0 || len(group.NetworkEntryIDs) != 1 {
		t.Fatalf("group=%+v", group)
	}
	if err := validateNodeGroupMembershipAvailability(f.h.db, group); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"expected_revision":0,"network_entry_ids":[]}`, 409},
		{`{"expected_revision":1,"network_entry_ids":[999999]}`, 400},
		{`{"expected_revision":1,"network_entry_ids":[]}`, 400},
	} {
		w := httptest.NewRecorder()
		f.h.NodeGroupUpdateHandler(w, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/node-groups/%d", group.ID), f.admin, tc.body))
		if w.Code != tc.status {
			t.Fatalf("update %s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	// Failed replacements roll back membership changes.
	if err := loadNodeGroupNetworkEntryIDs(f.h.db, &group); err != nil {
		t.Fatal(err)
	}
	if len(group.NetworkEntryIDs) != 1 {
		t.Fatal("failed mutation removed grant")
	}
	// Removing direct B access is allowed while the same group still has a front.
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: b.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		if err := replaceNodeGroupEndpoints(tx, group.ID, nil); err != nil {
			return err
		}
		return validateNodeGroupMembershipAvailability(tx, group)
	}); err != nil {
		t.Fatal(err)
	}
}
