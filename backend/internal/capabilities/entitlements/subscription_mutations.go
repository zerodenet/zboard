package entitlements

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrSubscriptionMutationInvalid = errors.New("invalid subscription mutation")
var ErrSubscriptionMutationConflict = errors.New("subscription mutation idempotency conflict")
var ErrSubscriptionMutationUnavailable = errors.New("subscription mutation unavailable")

const (
	SubscriptionExtend = "term.extend"
	SubscriptionCancel = "status.cancel"
)

type SubscriptionMutationInput struct {
	SubscriptionID uint
	Kind           string
	Days           int
	Reason         string
	IdempotencyKey string
	Origin         string
}

type SubscriptionMutationReceipt struct {
	MutationID     uint      `json:"mutation_id"`
	SubscriptionID uint      `json:"subscription_id"`
	Status         string    `json:"status"`
	EndAt          time.Time `json:"end_at"`
}

type SubscriptionMutationRepository interface {
	Apply(context.Context, uint, SubscriptionMutationInput) (SubscriptionMutationReceipt, error)
}

type SubscriptionMutations struct {
	Repository SubscriptionMutationRepository
}

// Apply validates one narrow entitlement command. The store owns the admin
// recheck, row lock, idempotency, credential/publication changes and audit.
func (s SubscriptionMutations) Apply(ctx context.Context, actor uint, input SubscriptionMutationInput) (SubscriptionMutationReceipt, error) {
	if actor == 0 {
		return SubscriptionMutationReceipt{}, ErrAccessPermission
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.SubscriptionID == 0 || len(input.Reason) < 3 || len(input.Reason) > 255 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || len(input.Origin) > 191 {
		return SubscriptionMutationReceipt{}, ErrSubscriptionMutationInvalid
	}
	switch input.Kind {
	case SubscriptionExtend:
		if input.Days < 1 || input.Days > 3650 {
			return SubscriptionMutationReceipt{}, ErrSubscriptionMutationInvalid
		}
	case SubscriptionCancel:
		if input.Days != 0 {
			return SubscriptionMutationReceipt{}, ErrSubscriptionMutationInvalid
		}
	default:
		return SubscriptionMutationReceipt{}, ErrSubscriptionMutationInvalid
	}
	if s.Repository == nil {
		return SubscriptionMutationReceipt{}, ErrSubscriptionMutationUnavailable
	}
	return s.Repository.Apply(ctx, actor, input)
}
