package networkstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func configurationPublicationFixture(t *testing.T) (*ConfigurationPublicationState, model.Node, model.ProtocolEndpoint) {
	t.Helper()
	db, _ := administrationFixture(t)
	node := model.Node{ID: 1, Name: "publish-node", Config: "{}", LifecycleStatus: "active", NodeCredential: "old", NodeCredentialPrefix: "old-prefix"}
	endpoint := model.ProtocolEndpoint{
		ID: 1, NodeID: node.ID, Name: "mieru", Protocol: "mieru", Address: "node.example.test", Port: 443, PublicPort: 443,
		MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true,
	}
	for _, row := range []interface{}{&model.User{ID: 7, Email: "publication-admin@example.test", Password: "hash", Status: "active", IsAdmin: true}, &node, &endpoint} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &ConfigurationPublicationState{DB: db}, node, endpoint
}

func TestConfigurationPublicationPersistsLifecycleAndReadiness(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx := context.Background()
	startedAt := time.Now().UTC()
	started, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID, RequestedBy: 7}, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if started.MieruFallbackCount != 1 || started.Deployment.Status != "running" || started.Deployment.RequestedBy == nil || *started.Deployment.RequestedBy != 7 {
		t.Fatalf("start=%+v", started)
	}
	deployment, err := store.SetConfigurationPublicationDesired(ctx, started.Deployment, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateConfigurationConnectorCredential(ctx, deployment, network.KernelEncryptedCredential{Ciphertext: "new", Prefix: "new-prefix"}); err != nil {
		t.Fatal(err)
	}
	finished := startedAt.Add(time.Minute)
	completed, err := store.CompleteConfigurationPublication(ctx, deployment, network.ConfigurationPublicationCompletion{
		ConfigSHA: strings.Repeat("a", 64), Output: "applied", LastHealthyAt: finished,
		MieruAccess: true, ManagedPrincipalAccess: true, MieruFallbackCount: 1, SuppressMieruFallback: true,
	}, finished)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" || completed.AppliedConfigSHA256 != strings.Repeat("a", 64) || completed.FinishedAt == nil {
		t.Fatalf("deployment=%+v", completed)
	}
	var state model.NodeKernelState
	var storedEndpoint model.ProtocolEndpoint
	var storedNode model.Node
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&storedEndpoint, endpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&storedNode, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.Status != "healthy" || state.AppliedConfigSHA256 != strings.Repeat("a", 64) || !storedEndpoint.MieruPrincipalReady || storedNode.NodeCredential != "new" || storedNode.LastSyncAt == nil {
		t.Fatalf("state=%+v endpoint=%+v node=%+v", state, storedEndpoint, storedNode)
	}
}

func TestCapabilityDeferralPreservesAppliedConfigurationHealthAndReadiness(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	oldSHA := strings.Repeat("b", 64)
	if err := store.DB.Create(&model.NodeKernelState{NodeID: node.ID, Status: "healthy", Phase: "idle", ServiceStatus: "active", ControlStatus: "healthy", AppliedConfigSHA256: oldSHA, LastHealthyAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&endpoint).Update("mieru_principal_ready", true).Error; err != nil {
		t.Fatal(err)
	}
	start, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := store.SetConfigurationPublicationDesired(ctx, start.Deployment, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	deferred, err := store.DeferConfigurationPublication(ctx, deployment, "Direct dial unavailable", "current retained", now.Add(time.Second))
	if err != nil || deferred.Status != "deferred" || deferred.AppliedConfigSHA256 != "" {
		t.Fatalf("deferred=%+v err=%v", deferred, err)
	}
	var state model.NodeKernelState
	var current model.ProtocolEndpoint
	var currentNode model.Node
	store.DB.First(&state, "node_id = ?", node.ID)
	store.DB.First(&current, endpoint.ID)
	store.DB.First(&currentNode, node.ID)
	if state.Status != "healthy" || state.AppliedConfigSHA256 != oldSHA || state.DesiredConfigSHA256 != strings.Repeat("a", 64) || !current.MieruPrincipalReady || currentNode.NodeCredential != "old" || currentNode.LastSyncAt != nil {
		t.Fatalf("deferral mutated runtime facts: state=%+v endpoint=%+v node=%+v", state, current, currentNode)
	}
}

func TestConfigurationPublicationAllowsNullableTriggerEndpoint(t *testing.T) {
	store, node, _ := configurationPublicationFixture(t)
	started, err := store.BeginConfigurationPublication(context.Background(), network.ConfigurationPublicationRequest{NodeID: node.ID}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if started.Deployment.ProtocolEndpointID != 0 || started.Deployment.NodeID != node.ID {
		t.Fatalf("deployment=%+v", started.Deployment)
	}
	var count int64
	if err := store.DB.Model(&model.ProtocolDeployment{}).
		Where("id = ? AND protocol_endpoint_id IS NULL", started.Deployment.ID).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("nullable endpoint rows=%d", count)
	}
}

func TestConfigurationPublicationFailureAndCredentialRestoreAreFenced(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx := context.Background()
	started, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateConfigurationConnectorCredential(ctx, started.Deployment, network.KernelEncryptedCredential{Ciphertext: "new", Prefix: "new-prefix"}); err != nil {
		t.Fatal(err)
	}
	snapshot := network.KernelConnectorSnapshot{Credential: network.KernelEncryptedCredential{Ciphertext: "old", Prefix: "old-prefix"}, Version: "0.0.14", ActiveFlows: 2}
	if err := store.DB.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{"version": "0.0.3", "active_flows": 8}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreConfigurationConnectorCredential(ctx, started.Deployment, snapshot); err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailConfigurationPublication(ctx, started.Deployment, "activation failed", "remote output", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.Error != "activation failed" {
		t.Fatalf("deployment=%+v", failed)
	}
	var storedNode model.Node
	var state model.NodeKernelState
	if err := store.DB.First(&storedNode, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedNode.NodeCredential != "old" || storedNode.Version != "0.0.3" || storedNode.ActiveFlows != 8 || state.Status != "apply_failed" {
		t.Fatalf("node=%+v state=%+v", storedNode, state)
	}
	if err := store.ActivateConfigurationConnectorCredential(ctx, started.Deployment, network.KernelEncryptedCredential{Ciphertext: "stale", Prefix: "stale"}); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("stale activation error=%v", err)
	}
}

func TestConfigurationPublicationCompletionRollsBackAtomically(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx := context.Background()
	started, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	const callbackName = "test:reject_configuration_kernel_state"
	removed := false
	if err := store.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "node_kernel_states" {
			tx.AddError(errors.New("kernel state unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !removed {
			_ = store.DB.Callback().Update().Remove(callbackName)
		}
	})
	_, err = store.CompleteConfigurationPublication(ctx, started.Deployment, network.ConfigurationPublicationCompletion{
		ConfigSHA: "config", LastHealthyAt: time.Now().UTC(), MieruAccess: true, ManagedPrincipalAccess: true, SuppressMieruFallback: true,
	}, time.Now().UTC())
	if err == nil {
		t.Fatal("completion ignored kernel state failure")
	}
	var deployment model.ProtocolDeployment
	var storedNode model.Node
	var storedEndpoint model.ProtocolEndpoint
	if err := store.DB.First(&deployment, started.Deployment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&storedNode, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&storedEndpoint, endpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	if deployment.Status != "running" || storedNode.LastSyncAt != nil || storedEndpoint.MieruPrincipalReady {
		t.Fatalf("deployment=%+v node=%+v endpoint=%+v", deployment, storedNode, storedEndpoint)
	}
	if err := store.DB.Callback().Update().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	removed = true
}

func TestConfigurationCompletionRequiresItsPersistedImmutableTarget(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	assertImmutableConfigurationTarget(t, store, node, endpoint)
}
func assertImmutableConfigurationTarget(t *testing.T, store *ConfigurationPublicationState, node model.Node, endpoint model.ProtocolEndpoint) {
	t.Helper()
	ctx, now := t.Context(), time.Now().UTC()
	oldSHA, newSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	started, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	completion := network.ConfigurationPublicationCompletion{ConfigSHA: oldSHA, Output: "healthy with old result", LastHealthyAt: now, ManagedPrincipalAccess: true, MieruAccess: true, MieruFallbackCount: 1, SuppressMieruFallback: true}
	if _, err := store.CompleteConfigurationPublication(ctx, started.Deployment, completion, now); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("health without persisted target accepted: %v", err)
	}
	desired, err := store.SetConfigurationPublicationDesired(ctx, started.Deployment, oldSHA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConfigurationPublicationDesired(ctx, desired, oldSHA); err != nil {
		t.Fatalf("exact target replay rejected: %v", err)
	}
	if _, err := store.SetConfigurationPublicationDesired(ctx, desired, newSHA); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("remote target replaced under same identity: %v", err)
	}
	completion.ConfigSHA = newSHA
	if _, err := store.CompleteConfigurationPublication(ctx, desired, completion, now); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("mismatched result accepted: %v", err)
	}
	completion.ConfigSHA = oldSHA
	wrong := desired
	wrong.DesiredConfigSHA256 = newSHA
	if _, err := store.CompleteConfigurationPublication(ctx, wrong, completion, now); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("stale caller target accepted: %v", err)
	}
	var current model.ProtocolDeployment
	var state model.NodeKernelState
	var storedEndpoint model.ProtocolEndpoint
	var currentNode model.Node
	for _, read := range []struct {
		row any
		id  any
	}{{&current, desired.ID}, {&state, node.ID}, {&storedEndpoint, endpoint.ID}, {&currentNode, node.ID}} {
		if err := store.DB.First(read.row, read.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	if current.Status != "running" || current.DesiredConfigSHA256 != oldSHA || current.AppliedConfigSHA256 != "" || state.AppliedConfigSHA256 != "" || storedEndpoint.MieruPrincipalReady || currentNode.LastSyncAt != nil {
		t.Fatalf("invalid completion committed side effects: deployment=%+v state=%+v endpoint=%+v node=%+v", current, state, storedEndpoint, currentNode)
	}
	if _, err := store.CompleteConfigurationPublication(ctx, desired, completion, now); err != nil {
		t.Fatalf("confirmed exact result rejected: %v", err)
	}
}
