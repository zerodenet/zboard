package entitlements

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"strings"
)

const MaxQuotaTargets = 10000

var ErrQuotaRequestInvalid = errors.New("invalid quota adjustment request")
var ErrQuotaRequestConflict = errors.New("quota request idempotency key already exists")

type QuotaAdjustment struct {
	DeltaMB int64  `json:"delta_mb"`
	Reason  string `json:"reason"`
}
type QuotaScope struct {
	UserIDs         []uint `json:"user_ids,omitempty"`
	SubscriptionIDs []uint `json:"subscription_ids,omitempty"`
	AllActive       bool   `json:"all_active,omitempty"`
}
type QuotaRequestInput struct {
	Scope          QuotaScope
	Content        QuotaAdjustment
	IdempotencyKey string
	Priority       int
	MaxAttempts    int
	AutoRun        bool
}
type QuotaRequestRepository interface {
	Create(context.Context, uint, QuotaRequestInput) (jobs.BatchReceipt, error)
}
type QuotaRequests struct{ Repository QuotaRequestRepository }

func (s QuotaRequests) Create(ctx context.Context, actor uint, in QuotaRequestInput) (jobs.BatchReceipt, error) {
	if actor == 0 {
		return jobs.BatchReceipt{}, ErrAccessPermission
	}
	in.Content.Reason = strings.TrimSpace(in.Content.Reason)
	if in.Content.DeltaMB == 0 || in.Content.DeltaMB < -1000000000 || in.Content.DeltaMB > 1000000000 || len(in.Content.Reason) < 3 || len(in.Content.Reason) > 255 {
		return jobs.BatchReceipt{}, ErrQuotaRequestInvalid
	}
	if len(in.Scope.UserIDs) > MaxQuotaTargets || len(in.Scope.SubscriptionIDs) > MaxQuotaTargets {
		return jobs.BatchReceipt{}, ErrQuotaRequestInvalid
	}
	in.Scope.UserIDs = quotaIDs(in.Scope.UserIDs)
	in.Scope.SubscriptionIDs = quotaIDs(in.Scope.SubscriptionIDs)
	if !in.Scope.AllActive && len(in.Scope.UserIDs)+len(in.Scope.SubscriptionIDs) == 0 {
		return jobs.BatchReceipt{}, ErrQuotaRequestInvalid
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 3
	}
	if in.MaxAttempts < 1 || in.MaxAttempts > 10 || in.Priority < -100 || in.Priority > 100 {
		return jobs.BatchReceipt{}, ErrQuotaRequestInvalid
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.NewString()
	}
	if len(in.IdempotencyKey) > 128 {
		return jobs.BatchReceipt{}, ErrQuotaRequestInvalid
	}
	return s.Repository.Create(ctx, actor, in)
}
func quotaIDs(ids []uint) []uint {
	out := []uint{}
	seen := map[uint]bool{}
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
