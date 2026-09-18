package handler

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"time"
)

const adminTaskWorkers = 1
const adminTaskPoll = time.Second

func (h *handlers) enqueueAdminTask(id uint, claims *authClaims) error {
	if claims == nil {
		return h.services.QueueInternalBatch(context.Background(), id)
	}
	return h.services.BatchControl().Queue(context.Background(), claims.UserID, id)
}

// A bounded batch is acquired before execution. Pending intent is durable;
// drafts have no scheduled_at and are never picked up automatically.
func (h *handlers) StartAdminTaskWorker() {
	h.startScheduledJob("registration_messages", adminTaskPoll, func(ctx context.Context) error {
		_, err := h.services.RegistrationMessages(h.credentialCipher).Process(ctx)
		return err
	})
	h.startScheduledJob("admin_tasks", adminTaskPoll, func(ctx context.Context) error {
		ids, err := h.services.BatchLifecycle().Ready(ctx, adminTaskWorkers)
		if err != nil {
			return err
		}
		errorsSeen := []error{}
		for _, id := range ids {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lockID, err := h.services.ClaimBatch(ctx, 0, id, true)
			if err != nil {
				continue
			}
			errorsSeen = append(errorsSeen, h.runQueuedTask(ctx, id, lockID))
		}
		errorsSeen = append(errorsSeen, ctx.Err())
		return errors.Join(errorsSeen...)
	})
}

// Recheck scheduling under the same row lock used by claimTask.
func (h *handlers) claimQueuedTask(id uint) (string, error) { return h.claimTaskMode(id, nil, true) }

func (h *handlers) withTaskLease(id uint, token string, write func(*gorm.DB) error) error {
	return h.services.WithBatchLease(context.Background(), id, token, write)
}

func (h *handlers) runQueuedTask(ctx context.Context, id uint, token string) error {
	return h.executeClaimedTaskContext(ctx, id, token)
}
