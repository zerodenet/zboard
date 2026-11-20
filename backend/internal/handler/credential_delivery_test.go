package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/clause"
)

func TestProtocolRenameKeepsConfirmedSubscriptionDeliverySQLite(t *testing.T) {
	assertProtocolRenameKeepsConfirmedSubscriptionDelivery(t, newOrderFixture(t))
}

func TestProtocolRenameKeepsConfirmedSubscriptionDeliveryMySQL(t *testing.T) {
	assertProtocolRenameKeepsConfirmedSubscriptionDelivery(t, newMySQLBusinessOrderFixture(t))
}

func assertProtocolRenameKeepsConfirmedSubscriptionDelivery(t *testing.T, f orderFixture) {
	t.Helper()
	endpoint := attachOrderPublishEndpoint(t, f)
	server, err := f.h.credentialCipher.Encrypt(`{"type":"shadowsocks","cipher":"chacha20-ietf-poly1305","password":"fixture"}`)
	if err != nil {
		t.Fatal(err)
	}
	egress, err := f.h.credentialCipher.Encrypt(`{"type":"socks5","server":"out.example.test","port":1080}`)
	if err != nil {
		t.Fatal(err)
	}
	client := `{"type":"shadowsocks","server":"edge.example.test","port":21388,"cipher":"chacha20-ietf-poly1305"}`
	if err := f.h.db.Model(&endpoint).Updates(map[string]any{
		"name": "Japan SS [02]", "protocol": "shadowsocks", "address": "edge.example.test",
		"listen_address": "::", "port": 21388, "public_port": 21388,
		"server_config": server, "client_config": client, "delivery_revision": 1,
		"egress_protocol": "socks5", "egress_config": egress,
	}).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	var subscription model.Subscription
	if err := f.h.db.First(&subscription, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	confirmTestCredentialConfiguration(t, f.h, endpoint.NodeID)
	assertDelivery := func(name string) {
		t.Helper()
		nodes, err := f.h.buildProjectedSubscriptionManifestNodes(t.Context(), []model.Subscription{subscription}, subscriptionProjectionFilter{}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 || nodes[0].ID != endpoint.ID || nodes[0].Name != name || nodes[0].DeliveryStatus != "confirmed" {
			t.Fatalf("expected confirmed endpoint %d with name %q, got %d nodes", endpoint.ID, name, len(nodes))
		}
	}
	assertDelivery("Japan SS [02]")

	body, err := json.Marshal(protocolEndpointWriteReq{
		NodeID: endpoint.NodeID, Name: "Japan SS [03]", Protocol: "shadowsocks", Address: "edge.example.test",
		Port: 21388, PublicPort: 21388, MultiplierMilli: 1000,
		Config:       `{ "cipher": "chacha20-ietf-poly1305", "password": "fixture", "type": "shadowsocks" }`,
		ClientConfig: `{ "cipher": "chacha20-ietf-poly1305", "type": "shadowsocks", "port": 21388, "server": "edge.example.test" }`,
		EgressConfig: `{ "port": 1080, "server": "out.example.test", "type": "socks5" }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	f.h.ProtocolEndpointUpdateHandler(response, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", endpoint.ID), f.token, string(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("rename failed: HTTP %d %s", response.Code, response.Body.String())
	}
	assertDelivery("Japan SS [03]")
	var update struct {
		Data protocolEndpointMutationResponse
	}
	if err := json.Unmarshal(response.Body.Bytes(), &update); err != nil {
		t.Fatal(err)
	}
	if update.Data.PublishStatus != network.ProtocolEndpointPublishNotRequired {
		t.Fatalf("rename requested publication: %q", update.Data.PublishStatus)
	}
	var saved model.ProtocolEndpoint
	if err := f.h.db.First(&saved, endpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DeliveryRevision != 1 || !saved.ManagedPrincipalReady || saved.ServerConfig != server || saved.EgressConfig != egress || saved.ClientConfig != client {
		t.Fatal("rename changed the previously confirmed protocol configuration")
	}
}

func TestCredentialDeliverySQLiteDoesNotExposeNewPurchaseBeforeItsConfirmation(t *testing.T) {
	assertPurchasedCredentialDelivery(t, newOrderFixture(t))
}
func TestCredentialDeliveryMySQLDoesNotExposeNewPurchaseBeforeItsConfirmation(t *testing.T) {
	assertPurchasedCredentialDelivery(t, newMySQLBusinessOrderFixture(t))
}

func assertPurchasedCredentialDelivery(t *testing.T, f orderFixture) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	endpoint := attachOrderPublishEndpoint(t, f)
	encrypted, err := f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&endpoint).Update("server_config", encrypted).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Update("last_seen_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteName: "delivery-test", SiteURL: "https://panel.example.test", InstalledAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	first := f.paid(t, f.create(t, 0).ID)
	var firstSub model.Subscription
	if err := f.h.db.First(&firstSub, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	project := func(sub model.Subscription) []subscriptionManifestNode {
		t.Helper()
		nodes, err := f.h.buildProjectedSubscriptionManifestNodes(ctx, []model.Subscription{sub}, subscriptionProjectionFilter{}, now)
		if err != nil {
			t.Fatal(err)
		}
		return nodes
	}
	if nodes := project(firstSub); len(nodes) != 0 {
		t.Fatal("purchase used endpoint readiness as credential delivery proof", len(nodes))
	}
	var node model.Node
	if err := f.h.db.First(&node, endpoint.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.services.NodeCredentialReconciliation(f.h.credentialCipher, true).ReconcileNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	_, sha, targets, err := f.h.compileNodeRuntimeConfigWithCredentialTargets(ctx, node, "test-connector", "", false)
	if err != nil || len(targets.Credentials) != 1 {
		t.Fatal("actual compiler did not capture first purchase", len(targets.Credentials), err)
	}
	state := f.h.services.ConfigurationPublicationState()
	started, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesiredWithTargets(ctx, started.Deployment, sha, targets)
	if err != nil {
		t.Fatal(err)
	}
	// A later independent purchase arrives before the first target's receipt.
	second := f.paid(t, f.create(t, 0).ID)
	var secondSub model.Subscription
	if err := f.h.db.First(&secondSub, second.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, network.ConfigurationPublicationCompletion{ConfigSHA: sha, LastHealthyAt: now, ManagedPrincipalAccess: true, MieruAccess: true}); err != nil {
		t.Fatal(err)
	}
	nodes := project(firstSub)
	if len(nodes) != 1 || nodes[0].SubscriptionID != firstSub.ID || nodes[0].DeliveryStatus != "confirmed" {
		t.Fatalf("first binding did not become confirmed: nodes=%d", len(nodes))
	}
	if nodes := project(secondSub); len(nodes) != 0 {
		t.Fatal("old receipt confirmed later independent purchase", len(nodes))
	}
	// Neither a stale nor missing heartbeat removes prior confirmed access.
	for _, lastSeen := range []any{now.Add(-48 * time.Hour), nil} {
		if err := f.h.db.Model(&model.Node{}).Where("id = ?", node.ID).Update("last_seen_at", lastSeen).Error; err != nil {
			t.Fatal(err)
		}
		nodes := project(firstSub)
		if len(nodes) != 1 {
			t.Fatal("observation erased confirmed authorization", len(nodes))
		}
	}
	if err := f.h.db.Model(&model.Node{}).Where("id = ?", node.ID).Update("is_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if nodes := project(firstSub); len(nodes) != 0 {
		t.Fatal("confirmed delivery overrode explicit administrator stop", len(nodes))
	}

}

// confirmTestCredentialConfiguration simulates a verified remote result at the
// persistence boundary for projection tests. It compiles the actual source and
// uses real frozen-intent/confirmation transactions; it is not native or
// production acceptance and does not hide the preceding pending state.
func confirmTestCredentialConfiguration(t *testing.T, h *handlers, nodeID uint) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	var installation model.Installation
	result := h.db.Limit(1).Find(&installation)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.RowsAffected == 0 {
		if err := h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example.test", InstalledAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	} else if installation.SiteURL == "" {
		if err := h.db.Model(&installation).Update("site_url", "https://panel.example.test").Error; err != nil {
			t.Fatal(err)
		}
	}
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.services.NodeCredentialReconciliation(h.credentialCipher, true).ReconcileNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	_, sha, targets, err := h.compileNodeRuntimeConfigWithCredentialTargets(ctx, node, "test-connector", "", false)
	if err != nil || len(targets.Credentials)+len(targets.NetworkEntries) == 0 {
		t.Fatal("projection fixture has no compiled credential target", len(targets.Credentials), err)
	}
	state := h.services.ConfigurationPublicationState()
	started, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: nodeID})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesiredWithTargets(ctx, started.Deployment, sha, targets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, network.ConfigurationPublicationCompletion{ConfigSHA: sha, LastHealthyAt: now, ManagedPrincipalAccess: true, MieruAccess: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCopiedProtocolDeliverySQLiteWaitsForMatchingConfiguration(t *testing.T) {
	assertCopiedProtocolDelivery(t, newOrderFixture(t))
}

func TestCopiedProtocolDeliveryMySQLWaitsForMatchingConfiguration(t *testing.T) {
	assertCopiedProtocolDelivery(t, newMySQLBusinessOrderFixture(t))
}

func assertCopiedProtocolDelivery(t *testing.T, f orderFixture) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	old := attachOrderPublishEndpoint(t, f)
	config, err := f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&old).Update("server_config", config).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	confirmTestCredentialConfiguration(t, f.h, old.NodeID)
	target := model.Node{Name: "protocol-copy-target", Address: "192.0.2.20", IsEnabled: true, LastSeenAt: &now, Config: "{}"}
	if err := f.h.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}

	service := f.h.services.ProtocolEndpointMutations(f.h.credentialCipher)
	source := network.ProtocolEndpointMutationRequest{
		NodeID: old.NodeID, Name: "Trojan original", Protocol: "trojan", Address: "192.0.2.11", Port: 8443, PublicPort: 8443,
		MultiplierMilli: 1000, ServerConfig: `{"type":"trojan","users":[]}`,
		ClientConfig: `{"type":"trojan","server":"192.0.2.11","port":8443}`, CredentialProtocols: []string{"trojan"},
	}
	if _, err := service.Save(ctx, 1, nil, source); err != nil {
		t.Fatal(err)
	}
	copyRequest := source
	copyRequest.NodeID, copyRequest.Name, copyRequest.Address = target.ID, "Trojan copy", target.Address
	copyRequest.ClientConfig = `{"type":"trojan","server":"192.0.2.20","port":8443}`
	copyRequest.MembershipChanges = []network.ProtocolEndpointMembershipChange{{NodeGroupID: f.group.ID, ExpectedRevision: f.group.Revision, Member: true}}
	created, err := service.Save(ctx, 1, nil, copyRequest)
	if err != nil || created.MembershipMutation == nil {
		t.Fatalf("copy did not persist its node-group delivery task: %v", err)
	}
	endpointID := created.ProtocolEndpoint.ID
	var nextEndpointID uint
	assertProjection := func(stage string, wantCopy bool) {
		t.Helper()
		rows, err := f.h.buildProjectedSubscriptionManifestNodes(ctx, []model.Subscription{sub}, subscriptionProjectionFilter{}, now)
		if err != nil {
			t.Fatal(err)
		}
		var oldVisible, copyVisible, nextVisible bool
		for _, endpoint := range rows {
			oldVisible = oldVisible || endpoint.ID == old.ID
			copyVisible = copyVisible || endpoint.ID == endpointID
			nextVisible = nextVisible || endpoint.ID == nextEndpointID
		}
		if !oldVisible || copyVisible != wantCopy || (nextEndpointID != 0 && nextVisible != wantCopy) {
			t.Fatalf("%s: original=%t first copy=%t second copy=%t, want copies visible=%t", stage, oldVisible, copyVisible, nextVisible, wantCopy)
		}
	}
	assertProjection("saved with a pre-existing node heartbeat", false)
	if f.h.db.Dialector.Name() == "mysql" {
		lock := f.h.db.WithContext(ctx).Begin()
		if lock.Error != nil {
			t.Fatal(lock.Error)
		}
		defer lock.Rollback()
		var locked model.ProtocolEndpoint
		if err := lock.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&locked, endpointID).Error; err != nil {
			t.Fatal(err)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		busy := f.h.reconcileNodeGroupCredentialsContext(probeCtx, f.group.ID)
		cancel()
		if !errors.Is(busy, network.ErrResourcesBusy) {
			t.Fatalf("holding the new endpoint row did not produce a retryable group conflict: %v", busy)
		}
		if err := lock.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		assertProjection("MySQL credential preparation was blocked by the endpoint lock", false)
	}

	if err := f.h.reconcileNodeGroupCredentialsContext(ctx, f.group.ID); err != nil {
		t.Fatal(err)
	}
	assertProjection("group credentials prepared", false)
	customization, err := json.Marshal(defaultSubscriptionCustomization(subscriptionRendererZnetSink))
	if err != nil {
		t.Fatal(err)
	}
	template := model.SubscriptionTemplate{
		Name: "ZNet Sink", Slug: subscriptionRendererZnetSink, Renderer: subscriptionRendererZnetSink,
		Customization: customization, IsActive: true, Revision: 1,
	}
	if err := f.h.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"is_active", "customization"}),
	}).Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	assertConnect := func(wantCopy bool) {
		t.Helper()
		summary := entitlements.SubscriptionSummary{
			ID: sub.ID, UserID: sub.UserID, PlanID: sub.PlanID, PlanSKUID: sub.PlanSKUID,
			NodeGroupID: sub.NodeGroupID, Status: sub.Status, EndAt: sub.EndAt, FlowTotal: sub.FlowTotal,
		}
		projected, err := f.h.ProjectPluginSubscription(ctx, summary, subscriptionRendererZnetSink, now)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(projected.Content)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(decoded), target.Address) != wantCopy {
			t.Fatalf("Connect returned mismatched copy visibility, want visible=%t", wantCopy)
		}
	}
	assertConnect(false)
	state := f.h.services.ConfigurationPublicationState()
	freeze := func(revision uint64) network.ConfigurationDeployment {
		t.Helper()
		_, sha, targets, err := f.h.compileNodeRuntimeConfigWithCredentialTargets(ctx, target, "test-connector", "", false)
		if err != nil || targets.ProtocolEndpoints[endpointID] != revision || (nextEndpointID != 0 && targets.ProtocolEndpoints[nextEndpointID] != 1) {
			t.Fatalf("actual compiler did not capture revision %d: targets=%+v error=%v", revision, targets.ProtocolEndpoints, err)
		}
		start, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: target.ID, TriggerEndpointID: endpointID})
		if err != nil {
			t.Fatal(err)
		}
		deployment, err := state.SetDesiredWithTargets(ctx, start.Deployment, sha, targets)
		if err != nil {
			t.Fatal(err)
		}
		return deployment
	}
	complete := func(deployment network.ConfigurationDeployment) {
		t.Helper()
		_, err := state.Complete(ctx, deployment, network.ConfigurationPublicationCompletion{
			ConfigSHA: deployment.DesiredConfigSHA256, LastHealthyAt: now, ManagedPrincipalAccess: true, MieruAccess: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	failure := errors.New("target failed to apply config")
	if _, err := state.Fail(ctx, freeze(1), failure, ""); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	assertProjection("publication failed", false)

	lostAck := network.ConfigurationOutcomeUnknown(errors.New("lost acknowledgement"))
	unconfirmed, err := state.Uncertain(ctx, freeze(1), lostAck, "")
	if !errors.Is(err, lostAck) {
		t.Fatal(err)
	}
	assertProjection("acknowledgement lost", false)
	if err := state.Resolve(ctx, unconfirmed, false, "config was not applied"); err != nil {
		t.Fatal(err)
	}
	assertProjection("kernel confirmed config was not applied", false)

	stale := freeze(1)
	snapshot, err := service.Load(ctx, 1, endpointID)
	if err != nil {
		t.Fatal(err)
	}
	copyRequest.ID = endpointID
	copyRequest.MembershipChanges = nil
	copyRequest.Port = 9443
	if _, err := service.Save(ctx, 1, &snapshot, copyRequest); err != nil {
		t.Fatal(err)
	}
	var currentGroup model.NodeGroup
	if err := f.h.db.First(&currentGroup, f.group.ID).Error; err != nil {
		t.Fatal(err)
	}
	nextCopy := copyRequest
	nextCopy.ID, nextCopy.Name, nextCopy.Port, nextCopy.PublicPort = 0, "Trojan second copy", 10443, 10443
	nextCopy.ClientConfig = `{"type":"trojan","server":"192.0.2.20","port":10443}`
	nextCopy.MembershipChanges = []network.ProtocolEndpointMembershipChange{{NodeGroupID: currentGroup.ID, ExpectedRevision: currentGroup.Revision, Member: true}}
	next, err := service.Save(ctx, 1, nil, nextCopy)
	if err != nil || next.MembershipMutation == nil {
		t.Fatalf("second copy did not persist its delivery task: %v", err)
	}
	nextEndpointID = next.ProtocolEndpoint.ID
	complete(stale)
	assertProjection("kernel acknowledgement for the previous listener and before the second copy", false)

	if err := f.h.reconcileNodeGroupCredentialsContext(ctx, f.group.ID); err != nil {
		t.Fatal(err)
	}
	complete(freeze(2))
	assertProjection("kernel acknowledged the current listener", true)
	assertConnect(true)
}
