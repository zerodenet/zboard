package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRealZeroNodeFailedPublicationSurvivesPublisherCrash(t *testing.T) {
	f, endpoint, run := newRealZeroNodeControlledFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	control := f.paid(t, f.create(t, 0).ID)
	secret, controlSecret := realZeroSecret(t, f, paid.SubscriptionID), realZeroSecret(t, f, control.SubscriptionID)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "initial recovery fixture publication", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("initial publication pending")
		}
		return realZeroEcho(secret)
	})
	f.h.CloseNodePublishWorker()
	// OpenSSH keeps this pre-existing management session alive when its listener
	// stops. New publisher connections must fail, while the data plane stays up.
	run("systemctl stop ssh.service")
	t.Cleanup(func() { run("systemctl start ssh.service") })
	sshAddress, _ := realZeroAddresses(t)
	if conn, err := net.DialTimeout("tcp", sshAddress, time.Second); err == nil {
		conn.Close()
		t.Fatal("SSH outage injection did not stop new connections")
	}
	if err := realZeroEcho(controlSecret); err != nil {
		t.Fatalf("data plane unavailable during SSH-only outage: %v", err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).
		Update("end_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	// Reconcile through the same production transaction as reads/expiry. This
	// scenario measures recovery, not the expiry scheduler's polling interval.
	if err := expireSubscriptions(f.h.db, paid.UserID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	firstPID, crash := startRealZeroPublisherProcess(t, f, "recovery-publisher-before-crash")
	var failed model.NodeConfigPublish
	waitRealZero(t, 30*time.Second, "failed real SSH publication persisted", func() error {
		if err := f.h.db.First(&failed, endpoint.NodeID).Error; err != nil {
			return err
		}
		if failed.Attempts == 0 || failed.LastError == "" || failed.LeaseToken != "" {
			return fmt.Errorf("failure not persisted yet")
		}
		return nil
	})
	crash()
	var retained model.NodeConfigPublish
	if err := f.h.db.First(&retained, endpoint.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Generation != failed.Generation || retained.Attempts != failed.Attempts || retained.LastError != failed.LastError {
		t.Fatal("pending failure changed or disappeared at process crash")
	}
	if err := realZeroEcho(secret); err != nil {
		t.Fatalf("stale node configuration no longer forwards before recovered publication: %v", err)
	}
	run("systemctl start ssh.service")
	restoredAt := time.Now()
	secondPID, stop := startRealZeroPublisherProcess(t, f, "recovery-publisher-after-crash")
	defer stop()
	if firstPID == secondPID {
		t.Fatal("publisher process was not replaced")
	}
	waitRealZero(t, 90*time.Second, "startup recovers revocation without a new request", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("retry remains pending")
		}
		if err := realZeroEcho(controlSecret); err != nil {
			return fmt.Errorf("positive control: %w", err)
		}
		for range 3 {
			if realZeroEcho(secret) == nil {
				return fmt.Errorf("old credential still forwards after recovered publication")
			}
		}
		return realZeroEcho(controlSecret)
	})
	result, err := json.Marshal(map[string]any{
		"scenario": "recovery", "passed": true, "outage": "ssh_listener_only", "crashed_component": "publisher_process",
		"stale_access_confirmed_before_recovery": true,
		"failed_attempts_persisted":              retained.Attempts, "failed_generation_retained": retained.Generation,
		"first_publisher_pid": firstPID, "replacement_publisher_pid": secondPID,
		"ssh_restored_to_verified_revocation_ms": float64(time.Since(restoredAt).Nanoseconds()) / 1e6,
		"new_request_after_crash":                false, "old_credential_rejected_probes": 3, "positive_control_passed": true, "verified_at": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}
