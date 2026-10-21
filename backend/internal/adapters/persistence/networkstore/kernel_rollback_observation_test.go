package networkstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestKernelRollbackPreservesObservedRuntime(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	kernelRollbackPreservesObservedRuntime(t)
}

func TestMySQLKernelRollbackPreservesObservedRuntime(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	kernelRollbackPreservesObservedRuntime(t)
}

func kernelRollbackPreservesObservedRuntime(t *testing.T) {
	t.Helper()
	store, admin, node, _ := kernelReconciliationFixture(t)
	ctx := context.Background()
	oldSeen := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if err := store.DB.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"is_enabled": true, "node_credential": "rollback-old", "node_credential_prefix": "old-prefix",
		"connector_last_seen_at": oldSeen, "last_seen_at": oldSeen,
		"is_online": true, "status": 1, "version": "old-observed", "uptime_seconds": 10,
		"active_flows": 3, "bytes_up": 20, "bytes_down": 30,
	}).Error; err != nil {
		t.Fatal(err)
	}
	operation, err := store.BeginReconciliation(ctx, network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, oldSeen)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := network.KernelConnectorSnapshot{
		Credential:          network.KernelEncryptedCredential{Ciphertext: "rollback-old", Prefix: "old-prefix"},
		ConnectorLastSeenAt: &oldSeen, LastSeenAt: &oldSeen, IsOnline: true, Status: 1,
		Version: "old-observed", UptimeSeconds: 10, ActiveFlows: 3, BytesUp: 20, BytesDown: 30,
	}
	if err := store.ActivateKernelConnectorCredential(ctx, operation, network.KernelEncryptedCredential{Ciphertext: "rollback-new", Prefix: "new-prefix"}); err != nil {
		t.Fatal(err)
	}
	// A real received event and heartbeat arrive after the operation's snapshot.
	// Neither is evidence that the operation's desired configuration was applied.
	newer := oldSeen.Add(30 * time.Second)
	position := network.EventPosition{AssetTag: node.AssetTag, ReceivedAt: newer, CoreInstanceID: "observed-runtime", Sequence: 50, ConfigRevision: 4, OccurredAt: newer}
	observation := network.NodeObservation{NodeID: node.ID, Latest: position, StatsEvent: &position,
		Stats: network.NodeStats{ActiveSessions: 55, BytesUp: 66, BytesDown: 77}}
	if err := store.DB.Transaction(func(tx *gorm.DB) error { return ProjectObservation(tx, observation) }); err != nil {
		t.Fatal(err)
	}
	if _, err := (observationstore.Worker{DB: store.DB}).Drain(ctx, 32, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	version, uptime := "new-observed", uint64(42)
	if err := (NodeActivity{DB: store.DB}).RecordNodeActivity(ctx, node.ID, "rollback-new", network.NodeActivityUpdate{
		At: newer, Online: false, ConnectorSeen: true, Version: &version, UptimeSeconds: &uptime,
	}); err != nil {
		t.Fatal(err)
	}
	row := func(target any, where string, args ...any) map[string]any {
		t.Helper()
		var rows []map[string]any
		if err := store.DB.Model(target).Where(where, args...).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected one persisted %T row, got %d", target, len(rows))
		}
		return rows[0]
	}
	digest := func(v any) [32]byte {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(raw)
	}
	// Full persisted maps include private fields omitted by the public model JSON.
	latestNode := row(&model.Node{}, "id = ?", node.ID)
	latestCursor := digest(row(&model.NodeObservationState{}, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag))
	callback := "test:fail_rollback_credential_update"
	injected := errors.New("credential restore persistence failure")
	if err := store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "nodes" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	callbackRemoved := false
	t.Cleanup(func() {
		if !callbackRemoved {
			_ = store.DB.Callback().Update().Remove(callback)
		}
	})
	if err := store.RestoreKernelConnectorCredential(ctx, operation, snapshot); !errors.Is(err, injected) {
		t.Fatalf("restore failure: %v", err)
	}
	if digest(row(&model.Node{}, "id = ?", node.ID)) != digest(latestNode) || digest(row(&model.NodeObservationState{}, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag)) != latestCursor {
		t.Fatal("failed restore changed node or observation cursor")
	}
	if err := store.DB.Callback().Update().Remove(callback); err != nil {
		t.Fatal(err)
	}
	callbackRemoved = true
	if err := store.RestoreKernelConnectorCredential(ctx, operation, snapshot); err != nil {
		t.Fatal(err)
	}
	current := row(&model.Node{}, "id = ?", node.ID)
	var restored model.Node
	if err := store.DB.First(&restored, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.NodeCredential != "rollback-old" || restored.NodeCredentialPrefix != "old-prefix" || restored.NodeCredentialRevokedAt != nil {
		t.Fatal("credential rollback did not restore exactly the previous identity")
	}
	for _, allowed := range []string{"node_credential", "node_credential_prefix", "node_credential_revoked_at", "updated_at"} {
		current[allowed] = latestNode[allowed]
	}
	if digest(current) != digest(latestNode) {
		t.Fatal("credential rollback rewound independently received node observations")
	}
	if digest(row(&model.NodeObservationState{}, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag)) != latestCursor {
		t.Fatal("credential rollback changed the observation cursor")
	}
	// Equal/older delivery remains a no-op against the preserved current values.
	observation.Stats = network.NodeStats{ActiveSessions: 999, BytesUp: 999, BytesDown: 999}
	if err := store.DB.Transaction(func(tx *gorm.DB) error { return ProjectObservation(tx, observation) }); err != nil {
		t.Fatal(err)
	}
	observation.Latest.Sequence--
	observation.Latest.OccurredAt = newer.Add(-time.Second)
	observation.StatsEvent = &observation.Latest
	if err := store.DB.Transaction(func(tx *gorm.DB) error { return ProjectObservation(tx, observation) }); err != nil {
		t.Fatal(err)
	}
	if _, err := (observationstore.Worker{DB: store.DB}).Drain(ctx, 32, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	beforeStale := row(&model.Node{}, "id = ?", node.ID)
	beforeStale["node_credential"] = latestNode["node_credential"]
	beforeStale["node_credential_prefix"] = latestNode["node_credential_prefix"]
	beforeStale["node_credential_revoked_at"] = latestNode["node_credential_revoked_at"]
	beforeStale["updated_at"] = latestNode["updated_at"]
	if digest(beforeStale) != digest(latestNode) {
		t.Fatal("replayed event changed preserved observations")
	}
	if err := store.DB.Model(&model.NodeKernelState{}).Where("node_id = ?", node.ID).Update("active_operation_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	frozen := digest(row(&model.Node{}, "id = ?", node.ID))
	if err := store.RestoreKernelConnectorCredential(ctx, operation, snapshot); !errors.Is(err, network.ErrKernelOperationLost) {
		t.Fatalf("stale restore: %v", err)
	}
	if digest(row(&model.Node{}, "id = ?", node.ID)) != frozen || digest(row(&model.NodeObservationState{}, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag)) != latestCursor {
		t.Fatal("stale restore changed node or cursor")
	}
}
