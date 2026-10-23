package commerce

import (
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

const PerpetualQuotaAllocation = "perpetual_paid_quota_v1"
const PerpetualQuotaFIFOAllocation = "perpetual_paid_quota_fifo_v1"

// A once purchase is one total-quota entitlement, not thousands of calendar
// periods ending at the storage sentinel. Its time proportion is always one.
// Unproven legacy grants and resets still require reconciliation; neither
// today's catalog nor current counters can establish their cash ancestry.
func projectSinglePerpetualQuotaValue(records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
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

// Each fulfilled grant is a separate paid lot. Consume cumulative increments
// before adding the next lot: an earlier overrun cannot spend a later purchase.
func projectPerpetualQuotaValue(records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
	if len(records) == 1 {
		return projectSinglePerpetualQuotaValue(records, current, currency, at)
	}
	empty := ValueProjection{Sources: map[uint]int64{}}
	root := records[0]
	if _, err := projectSinglePerpetualQuotaValue(records[:1], root.After, currency, at); err != nil {
		return empty, err
	}
	type lot struct {
		id                     uint
		quota, remaining, paid int64
	}
	lots := []lot{{root.OrderID, root.After.FlowTotal - root.After.CycleStartUsed, root.After.FlowTotal - root.After.CycleStartUsed, root.ActualPaidMinor}}
	consume := func(used int64) {
		for i := range lots {
			take := min(used, lots[i].remaining)
			lots[i].remaining -= take
			used -= take
			if used == 0 {
				return
			}
		}
	}
	ids := map[uint]bool{root.OrderID: true}
	last := root
	totalPaid := root.ActualPaidMinor
	for i, s := range records[1:] {
		if ids[s.OrderID] || s.OrderID == 0 || s.Version != 1 || s.SubscriptionID != root.SubscriptionID || s.Currency != currency || !s.PaymentVerified || s.LegacyTransferUnproven || s.ActualPaidMinor < 0 || s.CapturedAt.IsZero() || s.QuotaEventAt.IsZero() || s.QuotaEventID <= last.QuotaEventID || len(s.PaidPeriods) != 0 || s.Before == nil {
			return empty, ErrValueEvidence
		}
		// Fully captured old grants remain usable. Missing purchase/payment policy
		// is never reconstructed from today's catalog or current counters.
		validModel := s.Mode == "perpetual_quota_addition" && s.AllocationModel == PerpetualQuotaFIFOAllocation
		legacy := s.Mode == "unsupported_legacy_effect" && ((s.Kind == "renewal" && s.AllocationModel == "uniform_paid_units_v1") || (s.Kind == "traffic_pack" && s.AllocationModel == "legacy_value_requires_review"))
		if !validModel && !legacy {
			return empty, ErrValueEvidence
		}
		if (s.Kind != "renewal" && s.Kind != "traffic_pack") || (s.Kind == "renewal" && (s.BillingUnit != "once" || s.BillingValue != 1)) || !s.TermEndAt.Equal(root.TermEndAt) || s.TermStartAt.Before(root.TermStartAt) || at.Before(s.TermStartAt) {
			return empty, ErrValueEvidence
		}
		before, after := *s.Before, s.After
		if !samePerpetualStateIdentity(before, last.After) || before.FlowUsed < last.After.FlowUsed || before.FlowTotal < before.CycleStartUsed || !before.EndAt.Equal(after.EndAt) || !before.StartAt.Equal(after.StartAt) || before.PlanID != after.PlanID || before.CycleStartUsed != after.CycleStartUsed || before.FlowUsed != after.FlowUsed || after.FlowTotal <= before.FlowTotal || (after.ResetPolicy != 0 && after.ResetPolicy != 5) || after.NextResetAt != nil || after.EndedAt != nil || after.Status != "active" {
			return empty, ErrValueEvidence
		}
		if before.FlowTotal != last.After.FlowTotal {
			// The established single-lot quota-sync rule still applies before its first
			// addition. Mixed-lot unknown adjustments cannot invent a paid attribution.
			if i != 0 {
				return empty, ErrValueEvidence
			}
			lots[0].quota = before.FlowTotal - before.CycleStartUsed
			lots[0].remaining = lots[0].quota
		}
		consume(before.FlowUsed - last.After.FlowUsed)
		delta := after.FlowTotal - before.FlowTotal
		lots = append(lots, lot{s.OrderID, delta, delta, s.ActualPaidMinor})
		if totalPaid > math.MaxInt64-s.ActualPaidMinor {
			return empty, ErrValueEvidence
		}
		totalPaid += s.ActualPaidMinor
		ids[s.OrderID] = true
		last = s
	}
	finalIdentity := current
	if current.EndedAt != nil {
		// Normal exhausted lifecycle is evidence of zero remaining rights. Keep the
		// original terminal state in the quote fingerprint, never reopen a term.
		if current.EndReason != "exhausted" || current.FlowUsed < current.FlowTotal {
			return empty, ErrValueEvidence
		}
		finalIdentity.EndedAt = nil
	}
	if !samePerpetualStateIdentity(finalIdentity, last.After) || current.FlowUsed < last.After.FlowUsed || current.FlowTotal != last.After.FlowTotal {
		return empty, ErrValueEvidence
	}
	consume(current.FlowUsed - last.After.FlowUsed)
	out := ValueProjection{ValuationModel: PerpetualQuotaFIFOAllocation, RootOrderID: root.OrderID, Sources: map[uint]int64{}, CurrentCycle: ValueCycle{StartAt: root.TermStartAt, EndAt: root.TermEndAt, AmountMinor: totalPaid}}
	for _, l := range lots {
		amount := int64(0)
		if l.quota > 0 {
			amount = proportionalValue(l.paid, l.remaining, l.quota)
		}
		if out.CurrentMinor > math.MaxInt64-amount {
			return empty, ErrValueEvidence
		}
		out.CurrentMinor += amount
		if amount > 0 {
			out.Sources[l.id] = amount
		}
	}
	out.TotalMinor = out.CurrentMinor
	if current.EndedAt != nil && out.TotalMinor != 0 {
		return empty, ErrValueEvidence
	}
	return fingerprintValueProjection(records, current, out)
}

func samePerpetualStateIdentity(left, right EntitlementValueState) bool {
	return left.PlanID == right.PlanID && left.PlanSKUID == right.PlanSKUID && left.StartAt.Equal(right.StartAt) && left.EndAt.Equal(right.EndAt) && entitlements.IsPerpetualEnd(left.EndAt) && left.CycleStartUsed == right.CycleStartUsed && left.ResetPolicy == right.ResetPolicy && (left.ResetPolicy == 0 || left.ResetPolicy == 5) && left.NextResetAt == nil && right.NextResetAt == nil && left.EndedAt == nil
}
