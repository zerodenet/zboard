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

func kernelReconciliationFixture(t *testing.T) (*KernelReconciliationState, model.User, model.Node, model.ProtocolEndpoint) {
	t.Helper()
	detection, admin, node := kernelDetectionFixture(t)
	endpoint := model.ProtocolEndpoint{
		NodeID: node.ID, Name: "mieru-ready", Protocol: "mieru", Address: "node.example.test",
		Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerConfig: "{}", ClientConfig: "{}",
		OptionalConfig: "{}", Tags: "[]", IsActive: true,
	}
	if err := detection.DB.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	return &KernelReconciliationState{DB: detection.DB}, admin, node, endpoint
}

func healthyKernelProbe() network.KernelProbe {
	return network.KernelProbe{
		OperatingSystem: "debian 12", Architecture: "x86_64", Libc: "glibc 2.36", Systemd: true,
		Installed: true, Version: "0.0.15", BinarySHA256: strings.Repeat("a", 64), ConfigSHA256: strings.Repeat("b", 64),
		ServiceStatus: "active", ControlStatus: "healthy",
	}
}

func TestKernelReconciliationPersistsFencedLifecycleAndPublication(t *testing.T) {
	store, admin, node, endpoint := kernelReconciliationFixture(t)
	ctx := context.Background()
	started := time.Now().UTC()
	operation, err := store.BeginReconciliation(ctx, network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, started)
	if err != nil {
		t.Fatal(err)
	}
	operation, err = store.SetKernelPhase(ctx, operation, "detecting")
	if err != nil {
		t.Fatal(err)
	}
	probe := healthyKernelProbe()
	if err := store.RecordKernelProbe(ctx, operation, probe, started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	release := network.KernelRelease{Version: "0.0.15", ArtifactURL: "https://example.test/zero.tar.gz", ArtifactSHA256: strings.Repeat("c", 64)}
	operation, err = store.RecordKernelRelease(ctx, operation, release)
	if err != nil {
		t.Fatal(err)
	}
	operation, err = store.RecordKernelAction(ctx, operation, "upgrade")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CompleteKernelReconciliation(ctx, operation, probe, release, probe.BinarySHA256, probe.ConfigSHA256, "verified", true, true, "", started.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Status != "healthy" || result.State.ActiveOperationID != nil || result.Operation.Status != "succeeded" || result.Operation.OperationType != "upgrade" || !result.Changed || !result.ConnectorVerified {
		t.Fatalf("result=%+v", result)
	}
	var publication model.NodeConfigPublish
	if err := store.DB.First(&publication, node.ID).Error; err != nil || publication.EndpointID != endpoint.ID {
		t.Fatalf("publication=%+v err=%v", publication, err)
	}
	var storedNode model.Node
	if err := store.DB.First(&storedNode, node.ID).Error; err != nil || storedNode.Version != probe.Version || storedNode.SSHVerifiedAt == nil {
		t.Fatalf("node=%+v err=%v", storedNode, err)
	}
}

func TestKernelReconciliationCompletionRollsBackWhenPublicationFails(t *testing.T) {
	store, admin, node, _ := kernelReconciliationFixture(t)
	ctx := context.Background()
	operation, err := store.BeginReconciliation(ctx, network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	const callbackName = "test:reject_kernel_publication"
	callbackRemoved := false
	if err := store.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "node_config_publishes" {
			tx.AddError(errors.New("publication persistence unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !callbackRemoved {
			_ = store.DB.Callback().Create().Remove(callbackName)
		}
	})
	probe := healthyKernelProbe()
	release := network.KernelRelease{Version: probe.Version}
	if _, err := store.CompleteKernelReconciliation(ctx, operation, probe, release, "binary", "config", "verified", true, false, "not observed", time.Now().UTC()); err == nil {
		t.Fatal("kernel completion ignored publication failure")
	}
	var row model.NodeOperation
	var state model.NodeKernelState
	var storedNode model.Node
	if err := store.DB.First(&row, operation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&storedNode, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "running" || state.ActiveOperationID == nil || *state.ActiveOperationID != operation.ID || storedNode.Version != "" || storedNode.SSHVerifiedAt != nil {
		t.Fatalf("partial commit row=%+v state=%+v node=%+v", row, state, storedNode)
	}
	if err := store.DB.Callback().Create().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	callbackRemoved = true
	if _, err := store.CompleteKernelReconciliation(ctx, operation, probe, release, "binary", "config", "verified", true, false, "not observed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestKernelReconciliationFencesCredentialActivationAndRestoresSnapshot(t *testing.T) {
	store, admin, node, _ := kernelReconciliationFixture(t)
	ctx := context.Background()
	seenAt := time.Now().UTC().Add(-time.Minute)
	if err := store.DB.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{
		"node_credential": "old-connector", "node_credential_prefix": "old-prefix",
		"connector_last_seen_at": seenAt, "last_seen_at": seenAt, "is_online": true,
		"status": int16(2), "version": "0.0.14", "uptime_seconds": uint64(10),
		"active_flows": uint64(3), "bytes_up": uint64(20), "bytes_down": uint64(30),
	}).Error; err != nil {
		t.Fatal(err)
	}
	operation, err := store.BeginReconciliation(ctx, network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureKernelTrafficCredential(ctx, operation, network.KernelEncryptedCredential{Ciphertext: "traffic-secret", Prefix: "traffic"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateKernelConnectorCredential(ctx, operation, network.KernelEncryptedCredential{Ciphertext: "new-connector", Prefix: "new-prefix"}); err != nil {
		t.Fatal(err)
	}
	var activated model.Node
	if err := store.DB.First(&activated, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if activated.TrafficSecret != "traffic-secret" || activated.NodeCredential != "new-connector" || activated.NodeCredentialPrefix != "new-prefix" {
		t.Fatalf("activated node=%+v", activated)
	}
	snapshot := network.KernelConnectorSnapshot{
		Credential:          network.KernelEncryptedCredential{Ciphertext: "old-connector", Prefix: "old-prefix"},
		ConnectorLastSeenAt: &seenAt, LastSeenAt: &seenAt, IsOnline: true, Status: 2, Version: "0.0.14",
		UptimeSeconds: 10, ActiveFlows: 3, BytesUp: 20, BytesDown: 30,
	}
	if err := store.RestoreKernelConnectorCredential(ctx, operation, snapshot); err != nil {
		t.Fatal(err)
	}
	var restored model.Node
	if err := store.DB.First(&restored, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.NodeCredential != "old-connector" || restored.NodeCredentialPrefix != "old-prefix" || restored.ConnectorLastSeenAt == nil || !restored.ConnectorLastSeenAt.Equal(seenAt) || restored.Version != "0.0.14" || restored.ActiveFlows != 3 {
		t.Fatalf("restored node=%+v", restored)
	}
	if err := store.DB.Model(&model.NodeKernelState{}).Where("node_id = ?", node.ID).Update("active_operation_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateKernelConnectorCredential(ctx, operation, network.KernelEncryptedCredential{Ciphertext: "stale", Prefix: "stale"}); !errors.Is(err, network.ErrKernelOperationLost) {
		t.Fatalf("stale credential activation error=%v", err)
	}
}

func TestKernelReconciliationFencesStalePhaseAndFinalizesDowngradeBlock(t *testing.T) {
	store, admin, node, _ := kernelReconciliationFixture(t)
	ctx := context.Background()
	request := network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}
	operation, err := store.BeginReconciliation(ctx, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&model.NodeKernelState{}).Where("node_id = ?", node.ID).Update("active_operation_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetKernelPhase(ctx, operation, "detecting"); !errors.Is(err, network.ErrKernelOperationLost) {
		t.Fatalf("stale phase error=%v", err)
	}
	if err := store.DB.Model(&model.NodeOperation{}).Where("id = ?", operation.ID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	operation, err = store.BeginReconciliation(ctx, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	probe := healthyKernelProbe()
	release := network.KernelRelease{Version: "0.0.14"}
	if err := store.BlockKernelDowngrade(ctx, operation, probe, release, "config", "confirmation required", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var state model.NodeKernelState
	var row model.NodeOperation
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&row, operation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ActiveOperationID != nil || state.RecommendedAction != "manual_review" || row.Status != "failed" || row.Phase != "resolving_release" {
		t.Fatalf("state=%+v operation=%+v", state, row)
	}
}
