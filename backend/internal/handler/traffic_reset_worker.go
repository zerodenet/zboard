package handler

import (
	"context"
	"time"
)

const trafficResetPollInterval = time.Minute

func (h *handlers) StartTrafficResetWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startScheduledJob("traffic_reset", trafficResetPollInterval, func(ctx context.Context) error {
		if h.backgroundWorkPaused() {
			return nil
		}
		return h.services.TrafficReset(h.credentialCipher, h.zeroMieruAccess).RunDue(ctx, time.Now().UTC())
	})
}

func (h *handlers) CloseTrafficResetWorker() { h.closeScheduledJob("traffic_reset") }

func (h *handlers) runDueTrafficResets(now time.Time) error {
	if h.backgroundWorkPaused() {
		return nil
	}
	return h.services.TrafficReset(h.credentialCipher, h.zeroMieruAccess).RunDue(context.Background(), now)
}

func (h *handlers) resetDueSubscription(id uint, now time.Time) (bool, error) {
	return h.services.TrafficReset(h.credentialCipher, h.zeroMieruAccess).ResetDue(context.Background(), id, now)
}
