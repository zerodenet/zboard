package entitlements

import (
	"context"
	"errors"
	"strings"
)

var ErrSubscriptionQuotaInvalid = errors.New("invalid subscription quota")
var ErrSubscriptionQuotaConflict = errors.New("subscription quota idempotency conflict")
var ErrSubscriptionResetPolicyNotApplicable = errors.New("subscription does not support periodic reset")

// Values describe the current cycle, not historical traffic records.
type SubscriptionQuotaInput struct {
	SubscriptionID          uint   `json:"-"`
	FlowTotal               *int64 `json:"flow_total,omitempty"`
	FlowUsed                *int64 `json:"flow_used,omitempty"`
	ResetQuotaBytes         *int64 `json:"reset_quota_bytes,omitempty"`
	ResetPolicy             *int16 `json:"reset_policy,omitempty"`
	ExpectedFlowTotal       int64  `json:"expected_flow_total"`
	ExpectedFlowUsed        int64  `json:"expected_flow_used"`
	ExpectedResetQuotaBytes int64  `json:"expected_reset_quota_bytes"`
	Reason                  string `json:"reason"`
	IdempotencyKey          string `json:"idempotency_key"`
}
type SubscriptionQuotaRepository interface {
	Update(context.Context, uint, SubscriptionQuotaInput) error
}
type SubscriptionQuota struct{ Repository SubscriptionQuotaRepository }

func (s SubscriptionQuota) Update(ctx context.Context, actor uint, in SubscriptionQuotaInput) error {
	if actor == 0 {
		return ErrAdministrativeRead
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.SubscriptionID == 0 || len(in.Reason) < 3 || len(in.Reason) > 255 || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 128 {
		return ErrSubscriptionQuotaInvalid
	}
	// Legacy expected_* inputs remain accepted, but live usage is authoritative.
	if in.FlowTotal == nil && in.FlowUsed == nil && in.ResetQuotaBytes == nil && in.ResetPolicy == nil {
		return ErrSubscriptionQuotaInvalid
	}
	if in.ResetPolicy != nil && (*in.ResetPolicy < 0 || *in.ResetPolicy > 5) {
		return ErrSubscriptionQuotaInvalid
	}
	for _, value := range []*int64{in.FlowTotal, in.FlowUsed, in.ResetQuotaBytes} {
		if value != nil && (*value < 0 || *value > 9007199254740991) {
			return ErrSubscriptionQuotaInvalid
		}
	}
	if s.Repository == nil {
		return ErrSubscriptionMutationUnavailable
	}
	return s.Repository.Update(ctx, actor, in)
}
