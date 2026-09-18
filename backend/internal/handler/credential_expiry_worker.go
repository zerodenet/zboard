package handler

import (
	"context"
	"time"
)

const (
	credentialExpiryReconcileInterval  = 20 * time.Second
	credentialExpiryReconcileBatchSize = 200
)

func (h *handlers) StartCredentialExpiryWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startScheduledJob("credential_expiry", credentialExpiryReconcileInterval, func(ctx context.Context) error {
		// Drain a bounded burst so mass expiries do not wait another interval per batch.
		for batch := 0; batch < 10; batch++ {
			now := time.Now().UTC()
			expired, err := h.services.CredentialExpiry().ExpireDue(ctx, now, credentialExpiryReconcileBatchSize)
			if err != nil {
				return err
			}
			if len(expired) > 0 {
			}
			// Subscription count, not credential fanout, determines whether work remains.
			due, err := h.services.CredentialExpiry().HasDue(ctx, time.Now().UTC())
			if err != nil {
				return err
			}
			if !due {
				return nil
			}
		}
		return nil
	})
}

func (h *handlers) CloseCredentialExpiryWorker() { h.closeScheduledJob("credential_expiry") }

func (h *handlers) runExpiredCredentialReconciliation(now time.Time) error {
	if h.backgroundWorkPaused() {
		return nil
	}
	started := now.UTC()
	if started.IsZero() {
		started = time.Now().UTC()
	}
	expired, err := h.services.CredentialExpiry().ExpireDue(context.Background(), started, credentialExpiryReconcileBatchSize)
	if err != nil {
		return err
	}
	if len(expired) > 0 {
	}
	return nil
}
