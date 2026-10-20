package commerce

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

const PerpetualQuotaAllocation = "perpetual_paid_quota_v1"

// A once purchase is one total-quota entitlement, not thousands of calendar
// periods ending at the storage sentinel. Its time proportion is always one.
// Unknown legacy grants, resets and quota additions still require reconciliation;
// neither today's catalog nor current counters can establish their cash ancestry.
func projectPerpetualQuotaValue(records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
	empty := ValueProjection{Sources: map[uint]int64{}}
	if len(records) != 1 {
		return empty, ErrValueEvidence
	}
	s := records[0]
	if s.Version != 1 || s.OrderID == 0 || s.SubscriptionID == 0 || s.QuotaEventID == 0 || s.QuotaEventAt.IsZero() || s.CapturedAt.IsZero() || s.Currency != currency || !s.PaymentVerified || s.LegacyTransferUnproven || s.ActualPaidMinor < 0 || s.BillingUnit != "once" || s.BillingValue != 1 || len(s.PaidPeriods) != 0 {
		return empty, ErrValueEvidence
	}
	// Prior code captured verified once purchases under value_requires_review
	// solely because its calendar allocator rejected once. Accept only the same
	// complete immutable evidence; missing purchase policy is never repaired here.
	if s.AllocationModel != PerpetualQuotaAllocation && s.AllocationModel != "value_requires_review" {
		return empty, ErrValueEvidence
	}
	if (s.Mode != "new_term" && s.Mode != "recovery_term" && s.Mode != "replacement_term") || s.TermStartAt.IsZero() || !s.TermStartAt.Equal(s.After.StartAt) || !s.TermEndAt.Equal(s.After.EndAt) || !entitlements.IsPerpetualEnd(s.TermEndAt) || at.Before(s.TermStartAt) || !at.Before(s.TermEndAt) {
		return empty, ErrValueEvidence
	}
	if current.PlanID != s.After.PlanID || current.PlanSKUID != s.After.PlanSKUID || !current.StartAt.Equal(s.After.StartAt) || !current.EndAt.Equal(s.After.EndAt) || current.ResetPolicy != s.After.ResetPolicy || (current.ResetPolicy != 0 && current.ResetPolicy != 5) || current.NextResetAt != nil || s.After.NextResetAt != nil || current.CycleStartUsed != s.After.CycleStartUsed || current.FlowUsed < s.After.FlowUsed {
		return empty, ErrValueEvidence
	}
	quota, err := quotaForValue(current)
	if err != nil || s.After.FlowUsed != s.After.CycleStartUsed || s.After.FlowTotal <= s.After.CycleStartUsed {
		return empty, ErrValueEvidence
	}
	remaining := int64(0)
	if quota > 0 {
		used := current.FlowUsed - current.CycleStartUsed
		remaining = proportionalValue(s.ActualPaidMinor, quota-min(used, quota), quota)
	}
	out := ValueProjection{ValuationModel: PerpetualQuotaAllocation, RootOrderID: s.OrderID, CurrentMinor: remaining, TotalMinor: remaining, Sources: map[uint]int64{}, CurrentCycle: ValueCycle{StartAt: s.TermStartAt, EndAt: s.TermEndAt, AmountMinor: s.ActualPaidMinor}}
	if remaining > 0 {
		out.Sources[s.OrderID] = remaining
	}
	return fingerprintValueProjection(records, current, out)
}
