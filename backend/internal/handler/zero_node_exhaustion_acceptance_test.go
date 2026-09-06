package handler

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRealZeroNodeExhaustionRevokesDataPlane(t *testing.T) {
	f, endpoint := newRealZeroNodeFixture(t)
	const quota = int64(1024)
	if err := f.h.db.Model(&f.planRecord).Update("traffic_bytes", quota).Error; err != nil {
		t.Fatal(err)
	}
	paid := f.paid(t, f.create(t, 0).ID)
	// Each order captures its own entitlement. Keep a large control subscription
	// on the same listener to distinguish exhaustion from service interruption.
	if err := f.h.db.Model(&f.planRecord).Update("traffic_bytes", 100<<20).Error; err != nil {
		t.Fatal(err)
	}
	control := f.paid(t, f.create(t, 0).ID)
	secret, controlSecret := realZeroSecret(t, f, paid.SubscriptionID), realZeroSecret(t, f, control.SubscriptionID)
	f.h.StartNodePublishWorker()
	t.Cleanup(f.h.CloseNodePublishWorker)
	waitRealZero(t, 90*time.Second, "initial quota publication", func() error { return realZeroEcho(secret) })
	if err := realZeroEcho(controlSecret); err != nil {
		t.Fatalf("initial positive control: %v", err)
	}
	started := time.Now()
	var firstDenial time.Time
	successfulProbes := 1
	// Real echo sessions, never synthetic reports or a direct quota mutation,
	// generate the usage that must traverse Zero's webhook and panel accounting.
	for time.Since(started) < 30*time.Second {
		err := realZeroEcho(secret)
		if err != nil {
			if controlErr := realZeroEcho(controlSecret); controlErr == nil {
				firstDenial = time.Now()
				break
			}
		} else {
			successfulProbes++
		}
		time.Sleep(50 * time.Millisecond)
	}
	if firstDenial.IsZero() {
		t.Fatal("quota did not stop actual proxy traffic with control available")
	}
	var accountedAt time.Time
	waitRealZero(t, 30*time.Second, "real quota usage reconciled", func() error {
		var sub model.Subscription
		if err := f.h.db.First(&sub, paid.SubscriptionID).Error; err != nil {
			return err
		}
		if sub.FlowTotal != quota {
			return fmt.Errorf("order captured wrong quota: %d", sub.FlowTotal)
		}
		if sub.Status != subStatusExpired {
			return fmt.Errorf("quota status=%s used=%d", sub.Status, sub.FlowUsed)
		}
		var ledger int64
		if err := f.h.db.Model(&model.TrafficRecord{}).Where("subscription_id = ?", paid.SubscriptionID).
			Select("COALESCE(SUM(used_bytes),0)").Scan(&ledger).Error; err != nil {
			return err
		}
		if sub.FlowUsed != quota || ledger != quota {
			return fmt.Errorf("quota=%d used=%d ledger=%d", quota, sub.FlowUsed, ledger)
		}
		accountedAt = time.Now()
		return nil
	})
	waitRealZero(t, 90*time.Second, "exhaustion publication completed", func() error {
		var pending int64
		if err := f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", endpoint.NodeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return fmt.Errorf("quota revocation still queued")
		}
		if err := realZeroEcho(controlSecret); err != nil {
			return fmt.Errorf("positive control: %w", err)
		}
		for range 3 {
			if err := realZeroEcho(secret); err == nil {
				return fmt.Errorf("exhausted credential still forwards after publication")
			}
		}
		return realZeroEcho(controlSecret)
	})
	// The native kernel can enforce quota before the panel observes settlement.
	// Record that ordering without labelling a polling timestamp as DB commit.
	result, err := json.Marshal(map[string]any{
		"scenario": "exhaustion", "passed": true, "quota_bytes": quota, "subscription_used_bytes": quota, "ledger_bytes": quota,
		"successful_echo_probes": successfulProbes, "first_denial_after_load_ms": float64(firstDenial.Sub(started).Nanoseconds()) / 1e6,
		"accounting_observed_after_denial_ms":                float64(accountedAt.Sub(firstDenial).Nanoseconds()) / 1e6,
		"publication_confirmed_after_accounting_observed_ms": float64(time.Since(accountedAt).Nanoseconds()) / 1e6,
		"positive_control_passed":                            true, "rejected_probes_after_publication": 3, "verified_at": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ZBOARD_ZERO_ACCEPTANCE_RESULT=%s", result)
}
