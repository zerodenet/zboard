package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

// The runner supplies a Docker-internal node, a test-only key and its pinned
// host key. Normal test runs never contact a machine or start external work.
func TestRealZeroNodeExpiryRevokesDataPlane(t *testing.T) {
	f, endpoint := newRealZeroNodeFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	control := f.paid(t, f.create(t, 0).ID)
	secret := realZeroSecret(t, f, paid.SubscriptionID)
	controlSecret := realZeroSecret(t, f, control.SubscriptionID)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "initial publication and authenticated echo", func() error {
		return realZeroEcho(secret)
	})
	if err := realZeroEcho(controlSecret); err != nil {
		t.Fatalf("control credential cannot connect: %v", err)
	}
	waitRealZero(t, 20*time.Second, "real flow accounted", func() error {
		var sub model.Subscription
		if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
			return err
		}
		if sub.FlowUsed <= 0 {
			return fmt.Errorf("no real flow bytes recorded")
		}
		return nil
	})
	held, err := openRealZeroEcho(secret)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	expiredAt := time.Now().Add(-time.Second)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).
		Update("end_at", expiredAt).Error; err != nil {
		t.Fatal(err)
	}
	committedAt := time.Now()
	// Use the running production expiry worker, durable queue and SSH publisher.
	// A separate credential on the same inbound distinguishes revocation from an
	// offline listener, failed restart, or unreachable echo target.
	waitRealZero(t, 90*time.Second, "expired credential rejected with control still usable", func() error {
		var credential model.ProtocolCredential
		if err := f.h.db.Where("subscription_id = ?", paid.SubscriptionID).First(&credential).Error; err != nil {
			return err
		}
		if credential.Status != "expired" {
			return fmt.Errorf("expiry has not reconciled")
		}
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("revocation publication remains pending")
		}
		if err := realZeroEcho(controlSecret); err != nil {
			return fmt.Errorf("positive control failed: %w", err)
		}
		for range 3 {
			if err := realZeroEcho(secret); err == nil {
				return fmt.Errorf("expired credential still passed data")
			}
		}
		return nil
	})
	elapsed := time.Since(committedAt)
	t.Logf("business expiry commit to confirmed data-plane rejection: %s", elapsed)
	held.SetDeadline(time.Now().Add(2 * time.Second))
	_, writeErr := held.Write([]byte("after-expiry"))
	var buffer [32]byte
	count, readErr := held.Read(buffer[:])
	t.Logf("existing connection after publication: wrote_error=%t read_bytes=%d read_error=%v", writeErr != nil, count, readErr)
	if count > 0 {
		t.Fatal("old connection still relayed payload after restart publication")
	}
	// Every request in this test is locally generated; do not emit credential
	// material, headers, or runtime config contents into the acceptance log.
	result, err := json.Marshal(map[string]any{"scenario": "expiry", "passed": true, "commit_to_rejection_ms": float64(elapsed.Nanoseconds()) / 1e6, "existing_connection_read_bytes": count, "existing_connection_read_error": fmt.Sprint(readErr), "verified_at": time.Now().UTC(), "real_flow_accounted": true, "positive_control_passed": true, "rejected_probes": 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}

func realZeroSecret(t *testing.T, f orderFixture, subscriptionID uint) string {
	t.Helper()
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", subscriptionID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := f.h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func waitRealZero(t *testing.T, timeout time.Duration, description string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		last = check()
		if last == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("%s: %v", description, last)
}

func realZeroAddresses(t *testing.T) (string, string) {
	t.Helper()
	if os.Getenv("ZBOARD_TEST_ZERO_NODE") != "isolated-docker" {
		t.Skip("use the isolated real Zero acceptance runner")
	}
	sshAddress, proxyAddress := os.Getenv("ZBOARD_TEST_ZERO_SSH_ADDR"), os.Getenv("ZBOARD_TEST_ZERO_PROXY_ADDR")
	for _, address := range []string{sshAddress, proxyAddress} {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "zero-acceptance-node" {
			t.Fatal("real Zero tests require the runner's isolated Docker node alias")
		}
	}
	return sshAddress, proxyAddress
}
