package handler

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRealZeroNodeGroupChangeReplacesDataPlaneAccess(t *testing.T) {
	f, oldEndpoint := newRealZeroNodeFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	control := f.paid(t, f.create(t, 0).ID)
	oldSecret, controlSecret := realZeroSecret(t, f, paid.SubscriptionID), realZeroSecret(t, f, control.SubscriptionID)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "old group initially usable", func() error { return realZeroEcho(oldSecret) })
	if err := realZeroEcho(controlSecret); err != nil {
		t.Fatal(err)
	}
	held, err := openRealZeroEcho(oldSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	group := model.NodeGroup{Name: "Real replacement", Code: "real-replacement", IsEnabled: true}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	// Two disjoint groups use two real listeners on the isolated physical node.
	// The old group's control remains authorized through the same publication.
	var replacement model.ProtocolEndpoint
	if err := f.h.db.First(&replacement, oldEndpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	replacement.ID, replacement.RuntimeKey = 0, "65c04794-3e3f-4b9b-a662-d522cb03df36"
	replacement.Name, replacement.Port = "replacement-vless", 8444
	if err := f.h.db.Create(&replacement).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: replacement.ID}).Error; err != nil {
		t.Fatal(err)
	}
	f.group = group
	f.planRecord = f.plan(t, 2)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100 << 20, "device_limit": 64}).Error; err != nil {
		t.Fatal(err)
	}
	f.skuRecord = f.sku(t, f.planRecord.ID, 200, skuOperationChange)
	change := f.create(t, paid.SubscriptionID)
	f.paid(t, change.ID)
	committedAt := time.Now()
	var newCredential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", paid.SubscriptionID, replacement.ID).First(&newCredential).Error; err != nil {
		t.Fatal(err)
	}
	newSecret, err := f.h.credentialCipher.Decrypt(newCredential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	newEcho := func(secret string) error {
		conn, err := openRealZeroEchoAt(secret, "zero-acceptance-node:8444")
		if err != nil {
			return err
		}
		return conn.Close()
	}
	waitRealZero(t, 90*time.Second, "new group usable and old access removed", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", oldEndpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("group change publication pending")
		}
		if err := newEcho(newSecret); err != nil {
			return fmt.Errorf("new group: %w", err)
		}
		if err := realZeroEcho(controlSecret); err != nil {
			return fmt.Errorf("old group control: %w", err)
		}
		for range 3 {
			if realZeroEcho(oldSecret) == nil {
				return fmt.Errorf("old group still forwards changed subscription")
			}
		}
		if newEcho(controlSecret) == nil || realZeroEcho(newSecret) == nil {
			return fmt.Errorf("credential crossed group listener boundary")
		}
		return newEcho(newSecret)
	})
	elapsed := time.Since(committedAt)
	held.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = held.Write([]byte("after-group-change"))
	var buffer [32]byte
	n, readErr := held.Read(buffer[:])
	if n != 0 {
		t.Fatal("old established connection still forwards after publication")
	}
	result, err := json.Marshal(map[string]any{
		"scenario": "group_change", "passed": true, "physical_nodes": 1, "disjoint_group_listeners": 2,
		"order_handler_return_to_verified_access_ms": float64(elapsed.Nanoseconds()) / 1e6,
		"old_group_positive_control_passed":          true, "new_group_echo_passed": true, "cross_group_credentials_rejected": true,
		"old_credential_rejected_probes": 3, "existing_connection_read_bytes": n, "existing_connection_read_error": fmt.Sprint(readErr), "verified_at": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}
