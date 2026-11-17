package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observabilitystore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSubscriptionMaterialMigrationSQLite(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertSubscriptionMaterialMigration(t, h, c)
}
func TestSubscriptionMaterialMigrationMySQL(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertSubscriptionMaterialMigration(t, h, c)
}

func materialFixture(t *testing.T, h *handlers, c model.ProtocolCredential) (model.Subscription, []model.ProtocolEndpoint, uint) {
	t.Helper()
	var admin model.User
	if e := h.db.Where("is_admin=?", true).First(&admin).Error; e != nil {
		t.Fatal(e)
	}
	var sub model.Subscription
	if e := h.db.First(&sub, c.SubscriptionID).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&sub).Updates(map[string]any{"speed_limit_mbps": 0, "device_limit": 0}).Error; e != nil {
		t.Fatal(e)
	}
	var first model.ProtocolEndpoint
	if e := h.db.First(&first, c.ProtocolEndpointID).Error; e != nil {
		t.Fatal(e)
	}
	server, e := h.credentialCipher.Encrypt(`{"type":"shadowsocks","cipher":"aes-128-gcm","password":"legacy-fallback"}`)
	if e != nil {
		t.Fatal(e)
	}
	client := `{"method":"aes-128-gcm"}`
	if e := h.db.Model(&first).Updates(map[string]any{"protocol": "shadowsocks", "server_config": server, "client_config": client, "managed_principal_ready": true, "address": "node.example.test", "multiplier_milli": 1000}).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.First(&first, first.ID).Error; e != nil {
		t.Fatal(e)
	}
	second := model.ProtocolEndpoint{RuntimeKey: "a2b698ea-5c7a-4fb4-a94f-367fc4111a4a", NodeID: first.NodeID, Name: "second-ss", Protocol: "shadowsocks", Port: first.Port + 1, PublicPort: first.PublicPort + 1, IsActive: true, ManagedPrincipalReady: true, Address: first.Address, ServerConfig: server, ClientConfig: client, OptionalConfig: "{}", Tags: "[]", MultiplierMilli: 2000}
	if e := h.db.Create(&second).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: sub.NodeGroupID, ProtocolEndpointID: second.ID}).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&model.Node{}).Where("id=?", first.NodeID).Updates(map[string]any{"is_enabled": true, "last_seen_at": time.Now().UTC()}).Error; e != nil {
		t.Fatal(e)
	}
	confirmTestCredentialConfiguration(t, h, first.NodeID)
	return sub, []model.ProtocolEndpoint{first, second}, admin.ID
}

func assertSubscriptionMaterialMigration(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	ctx := context.Background()
	sub, endpoints, admin := materialFixture(t, h, c)
	service := h.services.CredentialMigration(h.credentialCipher, true)
	project := func() []subscriptionManifestNode {
		t.Helper()
		var current model.Subscription
		if e := h.db.First(&current, sub.ID).Error; e != nil {
			t.Fatal(e)
		}
		rows, e := h.buildProjectedSubscriptionManifestNodes(ctx, []model.Subscription{current}, subscriptionProjectionFilter{}, time.Now().UTC())
		if e != nil {
			t.Fatal(e)
		}
		return rows
	}
	before := project()
	if len(before) != 2 {
		t.Fatal("legacy endpoints", len(before))
	}
	var legacy []model.ProtocolCredential
	if e := h.db.Where("subscription_id=?", sub.ID).Find(&legacy).Error; e != nil {
		t.Fatal(e)
	}
	if e := service.Prepare(ctx, admin, sub.ID); e != nil {
		t.Fatal(e)
	}
	if e := service.Prepare(ctx, admin, sub.ID); e != nil {
		t.Fatal("idempotent prepare", e)
	}
	var root model.ProtocolCredential
	if e := h.db.Where("subscription_id=? AND material_version=1 AND current_scope=1", sub.ID).First(&root).Error; e != nil {
		t.Fatal(e)
	}
	var physical int64
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=?", sub.ID).Count(&physical).Error; e != nil || physical != 3 {
		t.Fatal("not one master", physical, e)
	}
	pending, e := service.Status(ctx, admin, sub.ID)
	if e != nil || pending.State != "awaiting_confirmation" || pending.PendingConfirmation != 2 || !pending.CanRollback {
		t.Fatal(pending, e)
	}
	if b, _ := json.Marshal(before); true {
		a, _ := json.Marshal(project())
		if !bytes.Equal(a, b) {
			t.Fatal("unconfirmed master replaced legacy client configuration")
		}
	}
	var node model.Node
	if e := h.db.First(&node, c.NodeID).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.services.NodeCredentialReconciliation(h.credentialCipher, true).ReconcileNode(ctx, node.ID); e != nil {
		t.Fatal(e)
	}
	one, hash, targets, e := h.compileNodeRuntimeConfigWithCredentialTargets(ctx, node, "test-connector", "", false)
	if e != nil {
		t.Fatal(e)
	}
	two, hash2, _, e := h.compileNodeRuntimeConfigWithCredentialTargets(ctx, node, "test-connector", "", false)
	if e != nil || hash != hash2 || !bytes.Equal(one, two) {
		t.Fatal("non-deterministic runtime material", e)
	}
	if len(targets.Credentials) != 4 {
		t.Fatal("dual credential targets", len(targets.Credentials))
	}
	if bytes.Contains(one, []byte(root.Secret)) || bytes.Contains(one, []byte(root.MaterialVerifier)) {
		t.Fatal("master leaked to node")
	}
	confirmTestCredentialConfiguration(t, h, node.ID)
	compatible, e := service.Status(ctx, admin, sub.ID)
	if e != nil || compatible.State != "compatibility" || compatible.CompatibleCredentials != 2 || compatible.CompatibilityUntil == nil {
		t.Fatal(compatible, e)
	}
	until := *compatible.CompatibilityUntil
	confirmTestCredentialConfiguration(t, h, node.ID)
	repeated, e := service.Status(ctx, admin, sub.ID)
	if e != nil || !repeated.CompatibilityUntil.Equal(until) {
		t.Fatal("confirmation extended compatibility", repeated, e)
	}
	if got := project(); len(got) != 2 {
		t.Fatal("confirmed master projection", len(got))
	}
	seed, e := h.credentialCipher.Decrypt(root.Secret)
	if e != nil {
		t.Fatal(e)
	}
	var principals []string
	for i, ep := range endpoints {
		principal, e := entitlements.CredentialBindingPrincipal(seed, entitlements.CredentialBinding{CredentialID: root.ID, NodeID: ep.NodeID, EndpointID: ep.ID})
		if e != nil {
			t.Fatal(e)
		}
		principals = append(principals, principal)
		mapped, found, e := meteringstore.ResolveCredential(h.db, ep.NodeID, principal)
		if e != nil || !found || mapped.ID != root.ID || mapped.ProtocolEndpointID != ep.ID {
			t.Fatal("flow-start mapping", mapped, found, e)
		}
		flow := metering.CompletedFlow{NodeID: ep.NodeID, CoreInstanceID: "shared-root", FlowID: fmt.Sprintf("endpoint-%d", ep.ID), PrincipalKey: principal, EventID: fmt.Sprintf("root-final-%d", i), EventType: "flow.completed", Sequence: uint64(i + 1), BytesUp: 10, BytesDown: 20, OccurredAt: time.Now().UTC(), InboundTag: fmt.Sprintf("endpoint-%d", ep.ID)}
		if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, flow); e != nil {
			t.Fatal("root billing", e)
		}
		if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, flow); e != nil {
			t.Fatal("root replay", e)
		}
	}
	assertAccountingTotal(t, h, sub.ID, 90, subStatusActive)
	// A connection key is shared, but its internal signed identity cannot cross
	// a node or endpoint to charge the wrong multiplier.
	if _, e := (credentialstore.PrincipalIndex{DB: h.db}).LookupPrepared(ctx, node.ID+1, principals[0]); e == nil {
		t.Fatal("node spoof accepted")
	}
	// No confirmation: old usable secrets remain recoverable after the deadline.
	expired := time.Now().UTC().Add(-2 * time.Second)
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND material_version=0", sub.ID).Updates(map[string]any{"compatibility_until": expired}).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := service.ProcessCompatibility(ctx, expired.Add(time.Second), 200); e != nil {
		t.Fatal(e)
	}
	for _, old := range legacy {
		var r model.ProtocolCredential
		if e := h.db.First(&r, old.ID).Error; e != nil || r.Secret == "" {
			t.Fatal("erased before removal confirmation", e)
		}
	}
	confirmTestCredentialConfiguration(t, h, node.ID)
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND material_version=0", sub.ID).UpdateColumn("updated_at", time.Now().UTC().Add(-2*time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := service.ProcessCompatibility(ctx, time.Now().UTC(), 200); e != nil {
		t.Fatal(e)
	}
	for i, old := range legacy {
		var r model.ProtocolCredential
		if e := h.db.First(&r, old.ID).Error; e != nil || r.Secret != "" || r.CurrentScope != nil {
			t.Fatalf("old secret not retired: id=%d status=%s has_secret=%v has_scope=%v deadline=%v revoked=%v updated=%v error=%v", r.ID, r.Status, r.Secret != "", r.CurrentScope != nil, r.CompatibilityUntil, r.RevokedAt, r.UpdatedAt, e)
		}
		flow := metering.CompletedFlow{NodeID: old.NodeID, CoreInstanceID: "late-old-client", FlowID: fmt.Sprintf("late-%d", old.ID), PrincipalKey: old.PrincipalKey, EventID: fmt.Sprintf("late-old-%d", old.ID), EventType: "flow.completed", Sequence: uint64(i + 1), BytesUp: 10, OccurredAt: expired.Add(-time.Minute), InboundTag: fmt.Sprintf("endpoint-%d", old.ProtocolEndpointID)}
		if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, flow); e != nil {
			t.Fatal("late legacy accounting lost", e)
		}
		if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, flow); e != nil {
			t.Fatal("late legacy replay", e)
		}
	}
	assertAccountingTotal(t, h, sub.ID, 120, subStatusActive)
	if e := service.Rollback(ctx, admin, sub.ID); e == nil {
		t.Fatal("expired compatibility rolled back")
	}
	details, e := h.services.SubscriptionQueries.Detail(ctx, admin, sub.ID)
	if e != nil || details.TotalCredentialCount != 1 || details.ActiveCredentialCount != 1 {
		t.Fatal("retired attribution aliases counted as connection credentials", details.TotalCredentialCount, details.ActiveCredentialCount, e)
	}
	// Adding an access point changes only existing membership/configuration;
	// it must not recreate the subscription x endpoint credential relation.
	third := endpoints[0]
	third.ID = 0
	third.RuntimeKey = "d76383e5-e734-48c7-a96e-28482bd060b3"
	third.Name = "third-ss"
	third.Port += 2
	third.PublicPort += 2
	if e := h.db.Create(&third).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: sub.NodeGroupID, ProtocolEndpointID: third.ID}).Error; e != nil {
		t.Fatal(e)
	}
	confirmTestCredentialConfiguration(t, h, node.ID)
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=?", sub.ID).Count(&physical).Error; e != nil || physical != 3 {
		t.Fatal("new endpoint created a credential row", physical, e)
	}
	if got := project(); len(got) != 3 {
		t.Fatal("new endpoint not delivered through the existing root", len(got))
	}
	// History expiry preserves the one current frozen source; derived state
	// rebuilds without root x endpoint confirmation rows.
	if _, e := (observabilitystore.HistoryRetention{DB: h.db}).Prune(ctx, observability.ProtocolDeployments, time.Now().Add(24*time.Hour), 200); e != nil {
		t.Fatal(e)
	}
	if e := h.db.Where("node_id=?", node.ID).Delete(&model.NodeCredentialDeliveryState{}).Error; e != nil {
		t.Fatal(e)
	}
	if e := datastore.RunMigrations(h.db); e != nil {
		t.Fatal("root delivery recovery after history expiry", e)
	}
	if got := project(); len(got) != 3 {
		t.Fatal("recovery lost delivered root", len(got))
	}
	var rootConfirmations int64
	if e := h.db.Model(&model.CredentialDeliveryConfirmation{}).Where("credential_id=?", root.ID).Count(&rootConfirmations).Error; e != nil || rootConfirmations != 0 {
		t.Fatal("root expanded persistent delivery state", rootConfirmations, e)
	}
	// Erased seed is unnecessary for validating a previously signed report.
	if e := h.db.Model(&root).Update("secret", "").Error; e != nil {
		t.Fatal(e)
	}
	if _, e := (credentialstore.PrincipalIndex{DB: h.db}).LookupPrepared(ctx, node.ID, principals[0]); e != nil {
		t.Fatal("historical verifier depends on seed", e)
	}
	late := metering.CompletedFlow{NodeID: node.ID, CoreInstanceID: "shared-root", FlowID: fmt.Sprintf("endpoint-%d", endpoints[0].ID), PrincipalKey: principals[0], EventID: "root-after-secret-erasure", EventType: "flow.completed", Sequence: 10, BytesUp: 30, BytesDown: 20, OccurredAt: expired.Add(-time.Minute)}
	if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, late); e != nil {
		t.Fatal("late root accounting after secret erasure", e)
	}
	assertAccountingTotal(t, h, sub.ID, 140, subStatusActive)
}

func TestSubscriptionMaterialRollbackKeepsLegacyAndHistoricalIdentity(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	sub, _, admin := materialFixture(t, h, c)
	svc := h.services.CredentialMigration(h.credentialCipher, true)
	if e := svc.Prepare(t.Context(), admin, sub.ID); e != nil {
		t.Fatal(e)
	}
	var root model.ProtocolCredential
	if e := h.db.Where("subscription_id=? AND material_version=1", sub.ID).First(&root).Error; e != nil {
		t.Fatal(e)
	}
	if e := svc.Rollback(t.Context(), admin, sub.ID); e != nil {
		t.Fatal(e)
	}
	if e := h.db.First(&root, root.ID).Error; e != nil || root.Secret != "" || root.MaterialVerifier == "" {
		t.Fatal("rollback historical authority", e)
	}
	if e := h.services.NodeCredentialReconciliation(h.credentialCipher, true).ReconcileNode(t.Context(), c.NodeID); e != nil {
		t.Fatal(e)
	}
	var count int64
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND current_scope=1 AND material_version=1", sub.ID).Count(&count).Error; e != nil || count != 0 {
		t.Fatal("rollback re-enabled", count, e)
	}
	if e := svc.Prepare(t.Context(), admin, sub.ID); e != nil {
		t.Fatal("restart migration", e)
	}
}

func TestSubscriptionMaterialBlockedTransitionKeepsExistingAccess(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	sub, endpoints, admin := materialFixture(t, h, c)
	svc := h.services.CredentialMigration(h.credentialCipher, true)
	initial, e := svc.Status(t.Context(), admin, sub.ID)
	if e != nil || initial.LegacyCredentials != 2 || initial.State != "legacy" {
		t.Fatal("legacy progress counts", initial, e)
	}
	if _, e := svc.Status(t.Context(), admin, sub.ID+1000000); !errors.Is(e, entitlements.ErrAccessNotFound) {
		t.Fatal("missing subscription did not return not-found", e)
	}
	if e := svc.Prepare(t.Context(), admin+1000000, sub.ID); !errors.Is(e, entitlements.ErrAccessPermission) {
		t.Fatal("non-administrator started migration", e)
	}
	encrypted, e := h.credentialCipher.Encrypt(`{"type":"shadowsocks","cipher":"2022-blake3-chacha20-poly1305"}`)
	if e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&endpoints[0]).Update("server_config", encrypted).Error; e != nil {
		t.Fatal(e)
	}
	if e := svc.Prepare(t.Context(), admin, sub.ID); !errors.Is(e, entitlements.ErrCredentialMigrationBlocked) {
		t.Fatal("one-user cipher migration accepted", e)
	}
	if e := h.db.Model(&endpoints[0]).Update("server_config", endpoints[1].ServerConfig).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Where("node_group_id=? AND protocol_endpoint_id=?", sub.NodeGroupID, endpoints[1].ID).Delete(&model.NodeGroupEndpoint{}).Error; e != nil {
		t.Fatal(e)
	}
	if e := h.db.Model(&sub).Update("device_limit", 3).Error; e != nil {
		t.Fatal(e)
	}
	if e := svc.Prepare(t.Context(), admin, sub.ID); !errors.Is(e, entitlements.ErrCredentialMigrationBlocked) {
		t.Fatal("dual principals doubled device allowance", e)
	}
	var roots int64
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND material_version=1", sub.ID).Count(&roots).Error; e != nil || roots != 0 {
		t.Fatal("blocked transition wrote a root", roots, e)
	}
	var old model.ProtocolCredential
	if e := h.db.First(&old, c.ID).Error; e != nil || old.Secret != c.Secret || old.PrincipalKey != c.PrincipalKey || old.CompatibilityUntil != nil {
		t.Fatal("blocked transition changed original access", e)
	}
}

func TestSubscriptionMaterialEnabledOnlyAffectsFreshSubscriptions(t *testing.T) {
	f := newOrderFixture(t)
	h, c := accountingFixtureFromOrder(t, f)
	_, _, _ = materialFixture(t, h, c)
	if e := h.db.Model(&model.SystemConfig{}).Where("config_key=?", "subscription_credential_material_enabled").Update("value", "true").Error; e != nil {
		t.Fatal(e)
	}
	if e := h.services.NodeCredentialReconciliation(h.credentialCipher, true).ReconcileNode(t.Context(), c.NodeID); e != nil {
		t.Fatal(e)
	}
	var legacyRoots int64
	if e := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND material_version=1", c.SubscriptionID).Count(&legacyRoots).Error; e != nil || legacyRoots != 0 {
		t.Fatal("implicitly migrated existing subscription", legacyRoots, e)
	}
	for i := 0; i < 2; i++ {
		paid := f.paid(t, f.create(t, 0).ID)
		var rows []model.ProtocolCredential
		if e := h.db.Where("subscription_id=?", paid.SubscriptionID).Find(&rows).Error; e != nil || len(rows) != 1 || rows[0].MaterialVersion != 1 {
			t.Fatal("fresh identity is not one root", len(rows), e)
		}
	}
	confirmTestCredentialConfiguration(t, h, c.NodeID)
}

func TestSubscriptionMaterialUpgradeMySQLPreservesExistingCredentials(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	var before model.ProtocolCredential
	if e := h.db.First(&before, c.ID).Error; e != nil {
		t.Fatal(e)
	}
	// Recreate the prior schema in this random, task-owned test database. No
	// business row or financial cursor is rewritten by the actual upgrade.
	if e := h.db.Exec("DELETE FROM schema_migrations WHERE version=?", "0093_subscription_credential_material.up.sql").Error; e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"DROP INDEX ux_subscription_credential_material ON protocol_credentials",
		"DROP INDEX idx_credential_compatibility_due ON protocol_credentials",
		"ALTER TABLE protocol_credentials DROP COLUMN material_group_id,DROP COLUMN material_version,DROP COLUMN material_scope,DROP COLUMN material_verifier,DROP COLUMN compatibility_started_at,DROP COLUMN compatibility_until,MODIFY node_id bigint unsigned NOT NULL,MODIFY protocol_endpoint_id bigint unsigned NOT NULL",
		"ALTER TABLE subscriptions DROP COLUMN credential_material_mode",
		"ALTER TABLE flow_cursor_checkpoints DROP COLUMN protocol_endpoint_id",
	} {
		if e := h.db.Exec(q).Error; e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e := datastore.RunMigrations(h.db); e != nil {
			t.Fatal("upgrade/restart", e)
		}
	}
	var after model.ProtocolCredential
	if e := h.db.First(&after, c.ID).Error; e != nil || after.Secret != before.Secret || after.PrincipalKey != before.PrincipalKey || after.CredentialID != before.CredentialID || after.NodeID != before.NodeID || after.ProtocolEndpointID != before.ProtocolEndpointID || after.EntitlementEpoch != before.EntitlementEpoch {
		t.Fatal("upgrade changed legacy identity", e)
	}
	assertSubscriptionMaterialMigration(t, h, c)
}
