package handler

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

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
