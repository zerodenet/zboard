package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRealZeroRecoveredPeriodRotatesAccessAndClosesOldFlow(t *testing.T) {
	f, endpoint := newRealZeroNodeFixture(t)
	paid := f.paid(t, f.create(t, 0).ID)
	control := f.paid(t, f.create(t, 0).ID)
	var original model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ? AND current_scope = ?", paid.SubscriptionID, 1).First(&original).Error; err != nil {
		t.Fatal(err)
	}
	oldSecret, controlSecret := realZeroSecret(t, f, paid.SubscriptionID), realZeroSecret(t, f, control.SubscriptionID)
	var sub model.Subscription
	if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	var token model.SubscriptionToken
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		var err error
		token, _, err = application.EnsureSubscriptionAccessToken(tx, sub, f.h.credentialCipher)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	initializeRealZeroNode(t, f, endpoint)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "initial period live", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("initial period delivery pending")
		}
		return realZeroEcho(oldSecret)
	})
	if err := realZeroEcho(controlSecret); err != nil {
		t.Fatal(err)
	}
	held, err := openRealZeroEcho(oldSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	f.h.CloseNodePublishWorker()
	// Force only the isolated business clock boundary; the public core continues
	// running the last confirmed configuration until the recovery is delivered.
	ended := time.Now().UTC().Add(-time.Minute)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"end_at": ended, "ended_at": nil, "end_reason": ""}).Error; err != nil {
		t.Fatal(err)
	}
	renewal := f.paid(t, f.create(t, sub.ID).ID)
	committed := time.Now()
	newSecret := realZeroSecret(t, f, sub.ID)
	if newSecret == oldSecret {
		t.Fatal("new period reused the old wire credential")
	}
	var recovered model.Subscription
	if err := f.h.db.First(&recovered, sub.ID).Error; err != nil || recovered.EntitlementEpoch != 1 || renewal.SubscriptionID != sub.ID {
		t.Fatal("period identity not preserved", err)
	}
	var sameToken model.SubscriptionToken
	if err := f.h.db.First(&sameToken, token.ID).Error; err != nil || sameToken.TokenHash != token.TokenHash || sameToken.TokenCiphertext != token.TokenCiphertext {
		t.Fatal("subscription URL changed", err)
	}
	if err := realZeroEcho(oldSecret); err != nil {
		t.Fatal("old confirmed data plane unexpectedly stopped before delivery", err)
	}
	// Deterministically invalidate only the recoverable identity projection
	// after synchronous completion preparation. The public core, event ingress,
	// quota facts, deduplication and canonical credentials stay real/intact.
	var injected atomic.Bool
	const callback = "native:epoch-principal-projection-race"
	if err := f.h.db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Context != context.Background() {
			return
		}
		if _, transactional := q.Statement.ConnPool.(gorm.TxCommitter); transactional {
			return
		}
		sql := q.Statement.SQL.String()
		if strings.HasPrefix(sql, "SELECT `id`") && strings.Contains(sql, "idx_credential_lookup_backfill") && injected.CompareAndSwap(false, true) {
			if err := f.h.db.Exec("UPDATE protocol_credentials SET principal_lookup_version = 0 WHERE id = ?", original.ID).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Query().Remove(callback) })
	f.h.StartNodePublishWorker()
	waitRealZero(t, 90*time.Second, "new period delivered and old period revoked", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("period delivery pending")
		}
		if err := realZeroEcho(newSecret); err != nil {
			return err
		}
		if err := realZeroEcho(controlSecret); err != nil {
			return fmt.Errorf("independent control: %w", err)
		}
		for range 3 {
			if realZeroEcho(oldSecret) == nil {
				return fmt.Errorf("old period still authenticates")
			}
		}
		return nil
	})
	held.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = held.Write([]byte("after-period-recovery"))
	var buffer [32]byte
	n, readErr := held.Read(buffer[:])
	if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
		t.Fatal("old flow closure remained unknown after recovery")
	}
	if readErr == nil {
		t.Fatal("old flow closure had no terminal read result")
	}
	if n != 0 {
		t.Fatal("old established flow survived fresh period delivery")
	}
	if !injected.Load() {
		t.Fatal("native completion projection race was not exercised")
	}
	var closed model.ClosedEntitlement
	if err := f.h.db.Where("subscription_id = ? AND epoch = ?", sub.ID, 0).Take(&closed).Error; err != nil || closed.TransitionOrderID != renewal.ID {
		t.Fatal("old quota evidence absent", err)
	}
	result, err := json.Marshal(map[string]any{"scenario": "renewal_epoch", "passed": true, "subscription_identity_preserved": true, "subscription_url_preserved": true, "fresh_wire_credential": true, "projection_invalidation_between_preparation_and_accounting": true, "entitlement_epoch": recovered.EntitlementEpoch, "old_credential_rejected_probes": 3, "existing_connection_read_bytes": n, "existing_connection_read_error": fmt.Sprint(readErr), "independent_control_passed": true, "order_handler_return_to_verified_access_ms": float64(time.Since(committed).Nanoseconds()) / 1e6, "expiry_boundary": "forced isolated database fixture; management and data plane are real", "verified_at": time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}
