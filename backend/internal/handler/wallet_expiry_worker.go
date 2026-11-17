package handler

import (
	"context"
	"time"
)

func (h *handlers) StartWalletReservationWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startScheduledJob("wallet_reservations", time.Minute, func(ctx context.Context) error {
		for batch := 0; batch < 4; batch++ {
			count, err := h.services.WalletPayments(h.credentialCipher, h.zeroMieruAccess).Expire(ctx, time.Now().UTC(), 200)
			if err != nil {
				return err
			}
			if count < 200 {
				return nil
			}
		}
		return nil
	})
}
