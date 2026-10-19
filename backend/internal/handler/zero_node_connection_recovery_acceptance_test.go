package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	cryptossh "golang.org/x/crypto/ssh"
	"net"
	"testing"
	"time"
)

func TestRealZeroConnectionVerificationSurvivesPublisherCrash(t *testing.T) {
	f, endpoint, run := newRealZeroNodeControlledFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	control := f.paid(t, f.create(t, 0).ID)
	secret, controlSecret := realZeroSecret(t, f, paid.SubscriptionID), realZeroSecret(t, f, control.SubscriptionID)
	initializeRealZeroNode(t, f, endpoint)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "initial publication", func() error {
		var n int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("initial publication pending")
		}
		return realZeroEcho(secret)
	})
	f.h.CloseNodePublishWorker()
	held, err := openRealZeroEcho(secret)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	run("cp /etc/ssh/sshd_config /tmp/zboard-flow-test-sshd && { printf '%s\\n' 'AllowStreamLocalForwarding no'; cat /tmp/zboard-flow-test-sshd; } > /etc/ssh/sshd_config && /usr/sbin/sshd -t && systemctl reload ssh.service")
	restore := func() {
		run("cp /tmp/zboard-flow-test-sshd /etc/ssh/sshd_config && /usr/sbin/sshd -t && systemctl reload ssh.service")
	}

	t.Cleanup(restore)
	run(`/usr/sbin/sshd -T | { flow_test_forbidden=no; while IFS= read -r setting; do case "$setting" in "allowstreamlocalforwarding no") flow_test_forbidden=yes ;; esac; done; test "$flow_test_forbidden" = yes; }`)
	run("test -S /run/zerodenet/control.sock && /usr/local/bin/zero status --json --socket /run/zerodenet/control.sock >/dev/null")

	node, err := f.h.loadNode(endpoint.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	waitRealZero(t, 10*time.Second, "reloaded SSH listener and explicit stream-local denial", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		probe, _, err := f.h.dialNodeSSHContext(ctx, node)
		if err != nil {
			return err
		}
		defer probe.Close()
		stop := context.AfterFunc(ctx, func() { probe.Close() })
		defer stop()
		forwarded, denied := probe.Dial("unix", "/run/zerodenet/control.sock")
		if denied == nil {
			forwarded.Close()
			return fmt.Errorf("native socket forwarding denial was not injected")
		}
		var channelError *cryptossh.OpenChannelError
		if !errors.As(denied, &channelError) || (channelError.Reason != cryptossh.Prohibited && channelError.Reason != cryptossh.ConnectionFailed) {
			return fmt.Errorf("native forwarding did not return a channel-open refusal")
		}
		return nil
	})

	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", paid.SubscriptionID).Update("end_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := expireSubscriptions(f.h.db, paid.UserID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	firstPID, crash := startRealZeroPublisherProcess(t, f, "connection-publisher-before-crash")
	var pending model.NodeConfigPublish
	waitRealZero(t, 90*time.Second, "applied configuration with denied connection verification", func() error {
		if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil {
			return err
		}
		if pending.Phase != "connections" || len(pending.AppliedConfigSHA256) != 64 || pending.LastError == "" || pending.LeaseToken != "" {
			return fmt.Errorf("connection phase not durable")
		}
		if realZeroEcho(secret) == nil {
			return fmt.Errorf("configuration did not revoke new access")
		}
		return realZeroEcho(controlSecret)
	})
	// Denial must be real: the old TCP flow is still usable before restoration.
	held.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := held.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if n, err := held.Read(b[:]); n != 1 || err != nil {
		t.Fatal("injected native-control denial did not preserve old flow", n, err)
	}
	var before int64
	if err := f.h.db.Model(&model.ProtocolDeployment{}).Where("node_id = ?", endpoint.NodeID).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	crash()
	restore()
	restoredAt := time.Now()
	secondPID, stop := startRealZeroPublisherProcess(t, f, "connection-publisher-after-crash")
	defer stop()
	if firstPID == secondPID {
		t.Fatal("publisher was not replaced")
	}
	waitRealZero(t, 90*time.Second, "connection-only recovery after process crash", func() error {
		var n int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("connection verification pending")
		}
		return realZeroEcho(controlSecret)
	})
	var after int64
	if err := f.h.db.Model(&model.ProtocolDeployment{}).Where("node_id = ?", endpoint.NodeID).Count(&after).Error; err != nil || after != before {
		t.Fatal("connection retry created another configuration deployment", before, after, err)
	}
	held.SetDeadline(time.Now().Add(2 * time.Second))
	held.Write([]byte("x"))
	n, readErr := held.Read(b[:])
	if n != 0 || readErr == nil {
		t.Fatal("old flow was not closed", n, readErr)
	}
	if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
		t.Fatal("timeout is not closure proof")
	}
	result, _ := json.Marshal(map[string]any{"scenario": "connection_recovery", "passed": true, "phase": "connections", "old_flow_usable_during_native_control_denial": true, "configuration_deployments_before_crash": before, "configuration_deployments_after_recovery": after, "first_publisher_pid": firstPID, "replacement_publisher_pid": secondPID, "new_business_request_after_crash": false, "existing_connection_read_bytes": n, "existing_connection_read_error": fmt.Sprint(readErr), "positive_control_passed": true, "restored_to_verified_closure_ms": float64(time.Since(restoredAt).Nanoseconds()) / 1e6, "verified_at": time.Now().UTC()})
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}
