package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

// TimedQuotaAdditionAllocation retains actual current-cycle value at a quota
// addition, rather than repricing consumed old quota or future paid periods.
const TimedQuotaAdditionAllocation = "timed_quota_addition_v1"

// ValueProjection is a refundable value calculation, not a wallet credit.
// A settlement must bind its fingerprint and consume its source budgets in the
// same transaction as lifecycle termination and durable revocation intent.
type ValueProjection struct {
	ValuationModel string         `json:"valuation_model"`
	RootOrderID    uint           `json:"root_order_id"`
	CurrentMinor   int64          `json:"current_minor,string"`
	FutureMinor    int64          `json:"future_minor,string"`
	TotalMinor     int64          `json:"total_minor,string"`
	Sources        map[uint]int64 `json:"-"`
	Fingerprint    string         `json:"fingerprint"`
	CurrentCycle   ValueCycle     `json:"-"`
}
type valueBasisEvidence struct {
	// The existing exact core payment clock only proves a bounded datetime(3)
	// round-up overlap; it never replaces the stored basis or billing precision.
	CoreCapturedAt        time.Time
	TimeOnlyWhenExhausted bool
	Basis                 RemainingValueBasis
	CycleStart            time.Time
	TotalQuota            int64
	Sources               map[uint]int64
}
type valueFrame struct {
	ctx     context.Context
	periods []PaidValuePeriod
	owners  []uint
	anchor  time.Time
	end     time.Time
	basis   *valueBasisEvidence
	cache   *valueFrameCycles
}

// One projection owns this cache. Frames only append immutable paid units;
// copying a pre-extension frame retains its earlier immutable partition.
type valueFrameCycles struct {
	periods int
	policy  int16
	cycles  []ValueCycle
	sources []map[uint]int64
}

func addValueAmounts(target map[uint]int64, source map[uint]int64) error {
	for id, amount := range source {
		if amount < 0 || target[id] > math.MaxInt64-amount {
			return ErrValueEvidence
		}
		target[id] += amount
	}
	return nil
}
func proportionValueSources(total int64, sources map[uint]int64) (map[uint]int64, error) {
	out := map[uint]int64{}
	ids := make([]uint, 0, len(sources))
	var weight int64
	for id, amount := range sources {
		if id == 0 || amount < 0 || weight > math.MaxInt64-amount {
			return nil, ErrValueEvidence
		}
		weight += amount
		ids = append(ids, id)
	}
	if total < 0 || total > weight {
		return nil, ErrValueEvidence
	}
	if weight == 0 {
		if total == 0 {
			return out, nil
		}
		return nil, ErrValueEvidence
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var cumulative, previous int64
	for _, id := range ids {
		cumulative += sources[id]
		rounded := cumulativeHalfUp(total, big.NewInt(cumulative), big.NewInt(weight))
		if rounded > previous {
			out[id] = rounded - previous
		}
		previous = rounded
	}
	return out, nil
}
func (f *valueFrame) cycles(policy int16) ([]ValueCycle, []map[uint]int64, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if f.cache != nil && f.cache.periods == len(f.periods) && f.cache.policy == policy {
		return f.cache.cycles, f.cache.sources, nil
	}
	cycles, err := splitPaidValueContext(f.ctx, f.periods, f.anchor, policy)
	if err != nil {
		return nil, nil, err
	}
	sources := make([]map[uint]int64, len(cycles))
	for i := range sources {
		sources[i] = map[uint]int64{}
	}
	if len(f.owners) != len(f.periods) {
		return nil, nil, ErrValueEvidence
	}
	for _, owner := range f.owners {
		if owner == 0 {
			return nil, nil, ErrValueEvidence
		}
	}
	if err := walkValueAllocations(f.ctx, f.periods, cycles, func(n, i int, delta int64) error {
		return addValueAmounts(sources[i], map[uint]int64{f.owners[n]: delta})
	}); err != nil {
		return nil, nil, err
	}
	f.cache = &valueFrameCycles{periods: len(f.periods), policy: policy, cycles: cycles, sources: sources}
	return cycles, sources, nil
}
func quotaForValue(s EntitlementValueState) (int64, error) {
	if s.FlowUsed < 0 || s.CycleStartUsed < 0 || s.FlowUsed < s.CycleStartUsed || s.FlowTotal < s.CycleStartUsed {
		return 0, ErrValueEvidence
	}
	return s.FlowTotal - s.CycleStartUsed, nil
}
func (f *valueFrame) project(s EntitlementValueState, at time.Time) (ValueProjection, error) {
	var out ValueProjection
	out.Sources = map[uint]int64{}
	cycles, sources, err := f.cycles(s.ResetPolicy)
	if err != nil {
		return out, err
	}
	quota, err := quotaForValue(s)
	if err != nil {
		return out, err
	}
	for i, c := range cycles {
		if err := f.ctx.Err(); err != nil {
			return out, err
		}
		if !at.Before(c.EndAt) {
			continue
		}
		if at.Before(c.StartAt) {
			if out.FutureMinor > math.MaxInt64-c.AmountMinor {
				return out, ErrValueEvidence
			}
			out.FutureMinor += c.AmountMinor
			if err := addValueAmounts(out.Sources, sources[i]); err != nil {
				return out, err
			}
			continue
		}
		out.CurrentCycle = c
		basis := RemainingValueBasis{At: c.StartAt, EndAt: c.EndAt, AmountMinor: c.AmountMinor, UsedAtBasis: s.CycleStartUsed, QuotaBytes: quota}
		basisSources := sources[i]
		valueAt := at
		if f.basis != nil && f.basis.CycleStart.Equal(c.StartAt) && f.basis.Basis.EndAt.Equal(c.EndAt) {
			basis = f.basis.Basis
			basisSources = f.basis.Sources
			// An already committed core payment can have a row rounded ahead
			// of its real capture instant. Bridge only that proved <=1ms
			// interval with the actual millisecond representation; a caller before capture or another discrepancy still
			// reaches the original TimeRemaining refusal below.
			captured := f.basis.CoreCapturedAt
			if at.Before(basis.At) && !captured.IsZero() && !at.Before(captured) && !basis.At.Before(captured) && basis.At.Equal(captured.Round(time.Millisecond)) {
				valueAt = basis.At
			}
			// Administrative quota synchronization preserves cumulative usage. Apply
			// the actual quota delta to the remaining-rights denominator, without
			// returning to a consumed pre-reset cash basis.
			change := quota - f.basis.TotalQuota
			if change > 0 && basis.QuotaBytes > math.MaxInt64-change {
				return out, ErrValueEvidence
			}
			basis.QuotaBytes += change
		}
		value := int64(0)
		if f.basis != nil && f.basis.TimeOnlyWhenExhausted && f.basis.CycleStart.Equal(c.StartAt) && f.basis.Basis.EndAt.Equal(c.EndAt) && basis.QuotaBytes <= 0 {
			if s.FlowUsed != basis.UsedAtBasis {
				return out, ErrValueEvidence
			}
			value, err = basis.TimeRemaining(valueAt)
			if err != nil {
				return out, err
			}
		}
		if basis.QuotaBytes > 0 {
			value, err = basis.Remaining(valueAt, s.FlowUsed)
			if err != nil {
				return out, err
			}
		}
		out.CurrentMinor = value
		shares, err := proportionValueSources(value, basisSources)
		if err != nil {
			return out, err
		}
		if err := addValueAmounts(out.Sources, shares); err != nil {
			return out, err
		}
	}
	if out.CurrentMinor > math.MaxInt64-out.FutureMinor {
		return out, ErrValueEvidence
	}
	out.TotalMinor = out.CurrentMinor + out.FutureMinor
	return out, nil
}
func appendValuePeriods(f *valueFrame, s EntitlementValueSnapshot) error {
	if len(s.PaidPeriods) == 0 || s.AllocationModel != "uniform_paid_units_v1" || !s.PaymentVerified || s.ActualPaidMinor < 0 || s.LegacyTransferUnproven {
		return ErrValueEvidence
	}
	var total int64
	for i, p := range s.PaidPeriods {
		if p.AmountMinor < 0 || total > math.MaxInt64-p.AmountMinor {
			return ErrValueEvidence
		}
		total += p.AmountMinor
		if i == 0 && !p.StartAt.Equal(s.TermStartAt) {
			return ErrValueEvidence
		}
		if i > 0 && !p.StartAt.Equal(s.PaidPeriods[i-1].EndAt) {
			return ErrValueEvidence
		}
		f.periods = append(f.periods, p)
		f.owners = append(f.owners, s.OrderID)
	}
	if total != s.ActualPaidMinor || !s.PaidPeriods[len(s.PaidPeriods)-1].EndAt.Equal(s.TermEndAt) {
		return ErrValueEvidence
	}
	return nil
}

// ProjectEntitlementValue replays serialized paid facts, then values only the
// current quota cycle and future paid cycles. It never scans traffic history.
// Persistence bounds and pages the serialized evidence before calling this
// pure calculation; the number of purchases is not a monetary eligibility rule.
func ProjectEntitlementValue(records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
	return ProjectEntitlementValueContext(context.Background(), records, current, currency, at)
}

// ProjectEntitlementValueContext keeps cancellation inside replay and interval
// allocation, so a canceled request cannot continue CPU work after database
// evidence has been read. It has the same monetary and fingerprint rules as the
// compatibility entrypoint; cancellation never returns a usable quote.
func ProjectEntitlementValueContext(ctx context.Context, records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
	empty := ValueProjection{Sources: map[uint]int64{}}
	if ctx == nil || len(records) == 0 || at.IsZero() {
		return empty, ErrValueEvidence
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	records = append([]EntitlementValueSnapshot(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].QuotaEventID < records[j].QuotaEventID })
	root := -1
	for i, s := range records {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if (s.Mode == "new_term" || s.Mode == "recovery_term" || s.Mode == "replacement_term") && s.After.StartAt.Equal(current.StartAt) {
			root = i
		}
	}
	if root < 0 {
		return empty, ErrValueEvidence
	}
	records = records[root:]
	if entitlements.IsPerpetualEnd(current.EndAt) {
		return projectPerpetualQuotaValue(ctx, records, current, currency, at)
	}
	result, _, err := projectTimedValueEvents(ctx, records, current, currency, at, nil, 0, false)
	return result, err
}

func projectTimedValueEvents(ctx context.Context, records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time, events []TimedPlanQuotaSyncEvent, epoch uint64, strict bool) (ValueProjection, *timedQuotaReplay, error) {
	empty := ValueProjection{Sources: map[uint]int64{}}
	frame := valueFrame{ctx: ctx, anchor: records[0].After.StartAt, end: records[0].After.EndAt}
	var tracker *timedQuotaReplay
	if strict {
		tracker = &timedQuotaReplay{ctx: ctx, epoch: epoch, events: events, frame: &frame}
	}
	var last EntitlementValueSnapshot
	for i, s := range records {
		if err := ctx.Err(); err != nil {
			return empty, nil, err
		}
		if s.Version != 1 || s.OrderID == 0 || s.QuotaEventID == 0 || s.QuotaEventAt.IsZero() || s.Currency != currency || !s.PaymentVerified || s.LegacyTransferUnproven || s.SubscriptionID != records[0].SubscriptionID || s.ActualPaidMinor < 0 || s.CapturedAt.IsZero() {
			return empty, nil, ErrValueEvidence
		}
		if i > 0 && (s.QuotaEventID <= last.QuotaEventID || s.Before == nil || !s.Before.StartAt.Equal(last.After.StartAt) || !s.Before.EndAt.Equal(last.After.EndAt) || s.Before.PlanID != last.After.PlanID || s.Before.PlanSKUID != last.After.PlanSKUID || s.Before.ResetPolicy != last.After.ResetPolicy || s.Before.FlowUsed < last.After.FlowUsed) {
			return empty, nil, ErrValueEvidence
		}
		eventAt := s.QuotaEventAt.UTC()
		if i == 0 {
			if (s.Mode != "new_term" && s.Mode != "recovery_term" && s.Mode != "replacement_term") || !s.TermStartAt.Equal(s.After.StartAt) || !s.TermEndAt.Equal(s.After.EndAt) {
				return empty, nil, ErrValueEvidence
			}
			if err := appendValuePeriods(&frame, s); err != nil {
				return empty, nil, err
			}
			if tracker != nil {
				if err := tracker.seed(s); err != nil {
					return empty, nil, err
				}
			}
			last = s
			continue
		}
		if tracker != nil {
			if err := tracker.beforeGrant(records[:i], s); err != nil {
				return empty, nil, err
			}
			var err error
			eventAt, err = tracker.grantAt(s)
			if err != nil {
				return empty, nil, err
			}
		}
		before, err := frame.project(*s.Before, eventAt)
		if err != nil {
			return empty, nil, err
		}
		switch s.Mode {
		case "extended_term":
			if s.Before.FlowUsed != s.After.FlowUsed || s.Before.FlowTotal != s.After.FlowTotal || s.Before.CycleStartUsed != s.After.CycleStartUsed || s.Before.ResetPolicy != s.After.ResetPolicy || !sameValueReset(s.Before.NextResetAt, s.After.NextResetAt) {
				return empty, nil, ErrValueEvidence
			}
			priorFrame := frame
			if !s.TermStartAt.Equal(s.Before.EndAt) || !s.TermEndAt.Equal(s.After.EndAt) || !s.After.EndAt.After(s.Before.EndAt) {
				return empty, nil, ErrValueEvidence
			}
			if err := appendValuePeriods(&frame, s); err != nil {
				return empty, nil, err
			}
			frame.end = s.After.EndAt
			cycles, sources, err := frame.cycles(s.After.ResetPolicy)
			if err != nil {
				return empty, nil, err
			}
			for n, c := range cycles {
				if eventAt.Before(c.StartAt) || !eventAt.Before(c.EndAt) {
					continue
				}
				added := sources[n][s.OrderID]
				if !c.EndAt.Equal(before.CurrentCycle.EndAt) || added > 0 {
					// Preserve the settled old remainder, add only actual new
					// current-cycle cash, then consume from this new instant.
					// Remaining quota is not replenished by a time extension.
					if before.CurrentMinor > math.MaxInt64-added {
						return empty, nil, ErrValueEvidence
					}
					shares, err := currentProjectionSources(before, priorFrame, *s.Before, eventAt)
					if err != nil {
						return empty, nil, err
					}
					if err := addValueAmounts(shares, map[uint]int64{s.OrderID: added}); err != nil {
						return empty, nil, err
					}
					totalQuota, err := quotaForValue(s.After)
					if err != nil {
						return empty, nil, err
					}
					remaining := max(int64(0), s.After.FlowTotal-s.After.FlowUsed)
					frame.basis = &valueBasisEvidence{CoreCapturedAt: s.CapturedAt, CycleStart: c.StartAt, TotalQuota: totalQuota, Sources: shares, TimeOnlyWhenExhausted: remaining == 0, Basis: RemainingValueBasis{At: eventAt, EndAt: c.EndAt, AmountMinor: before.CurrentMinor + added, UsedAtBasis: s.After.FlowUsed, QuotaBytes: remaining}}
				}
				break
			}
		case "timed_quota_addition":
			if s.Kind != "traffic_pack" || s.AllocationModel != TimedQuotaAdditionAllocation || len(s.PaidPeriods) != 0 ||
				!s.TermStartAt.Equal(s.CapturedAt) || !s.TermEndAt.After(eventAt) ||
				!s.Before.StartAt.Equal(s.After.StartAt) || !s.Before.EndAt.Equal(s.After.EndAt) ||
				s.Before.PlanID != s.After.PlanID || s.Before.PlanSKUID != s.After.PlanSKUID ||
				s.Before.ResetPolicy != s.After.ResetPolicy || s.Before.ResetQuotaBytes != s.After.ResetQuotaBytes ||
				!sameValueReset(s.Before.NextResetAt, s.After.NextResetAt) ||
				s.After.FlowUsed != s.Before.FlowUsed || s.After.CycleStartUsed != s.Before.CycleStartUsed ||
				s.After.FlowTotal <= s.Before.FlowTotal || !s.TermEndAt.Equal(before.CurrentCycle.EndAt) ||
				before.CurrentMinor > math.MaxInt64-s.ActualPaidMinor {
				return empty, nil, ErrValueEvidence
			}
			shares, err := currentProjectionSources(before, frame, *s.Before, eventAt)
			if err != nil {
				return empty, nil, err
			}
			if err := addValueAmounts(shares, map[uint]int64{s.OrderID: s.ActualPaidMinor}); err != nil {
				return empty, nil, err
			}
			quota, err := quotaForValue(s.After)
			if err != nil {
				return empty, nil, err
			}
			remaining := max(int64(0), s.After.FlowTotal-s.After.FlowUsed)
			if remaining == 0 {
				return empty, nil, ErrValueEvidence
			}
			frame.basis = &valueBasisEvidence{CoreCapturedAt: s.CapturedAt, CycleStart: before.CurrentCycle.StartAt, TotalQuota: quota, Sources: shares,
				Basis: RemainingValueBasis{At: eventAt, EndAt: before.CurrentCycle.EndAt, AmountMinor: before.CurrentMinor + s.ActualPaidMinor, UsedAtBasis: s.After.FlowUsed, QuotaBytes: remaining}}
		case "paid_reset":
			if s.AllocationModel != "reset_value_continuity_v1" || !s.Before.EndAt.Equal(s.After.EndAt) || s.Before.NextResetAt == nil || s.After.NextResetAt == nil || !s.Before.NextResetAt.Equal(*s.After.NextResetAt) || s.After.FlowUsed != s.Before.FlowUsed || s.After.CycleStartUsed != s.After.FlowUsed || before.CurrentMinor > math.MaxInt64-s.ActualPaidMinor {
				return empty, nil, ErrValueEvidence
			}
			// Only current-cycle sources survive the reset. Future paid allocations
			// remain separate and are never repriced or converted by the reset.
			shares, err := currentProjectionSources(before, frame, *s.Before, eventAt)
			if err != nil {
				return empty, nil, err
			}
			if err := addValueAmounts(shares, map[uint]int64{s.OrderID: s.ActualPaidMinor}); err != nil {
				return empty, nil, err
			}
			quota, err := quotaForValue(s.After)
			if err != nil {
				return empty, nil, err
			}
			frame.basis = &valueBasisEvidence{CoreCapturedAt: s.CapturedAt, CycleStart: before.CurrentCycle.StartAt, TotalQuota: quota, Sources: shares, Basis: RemainingValueBasis{At: eventAt, EndAt: before.CurrentCycle.EndAt, AmountMinor: before.CurrentMinor + s.ActualPaidMinor, UsedAtBasis: s.After.FlowUsed, QuotaBytes: quota}}
		default:
			return empty, nil, ErrValueEvidence
		}
		if tracker != nil {
			if err := tracker.acceptGrant(s); err != nil {
				return empty, nil, err
			}
		}
		last = s
	}
	if tracker != nil {
		if err := tracker.finish(records, current, at); err != nil {
			return empty, nil, err
		}
	}
	if current.PlanID != last.After.PlanID || current.PlanSKUID != last.After.PlanSKUID || !current.StartAt.Equal(last.After.StartAt) || !current.EndAt.Equal(last.After.EndAt) || current.ResetPolicy != last.After.ResetPolicy || current.FlowUsed < last.After.FlowUsed {
		return empty, nil, ErrValueEvidence
	}
	out, err := frame.project(current, at)
	if err != nil {
		return empty, nil, err
	}
	out.ValuationModel = "remaining_value_basis_v1"
	out.RootOrderID = records[0].OrderID
	if tracker != nil && tracker.typedSeen {
		out.ValuationModel = TimedPlanQuotaSyncAllocation
		out, err = tracker.fingerprint(current, out)
	} else {
		out, err = fingerprintValueProjection(ctx, records, current, out)
	}
	return out, tracker, err
}

func fingerprintValueProjection(ctx context.Context, records []EntitlementValueSnapshot, current EntitlementValueState, out ValueProjection) (ValueProjection, error) {
	if err := ctx.Err(); err != nil {
		return ValueProjection{}, err
	}
	payload, err := json.Marshal(struct {
		Records []EntitlementValueSnapshot
		State   EntitlementValueState
		Quote   ValueProjection
	}{records, current, out})
	if err != nil {
		return ValueProjection{}, err
	}
	if err := ctx.Err(); err != nil {
		return ValueProjection{}, err
	}
	sum := sha256.Sum256(payload)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, nil
}

func currentProjectionSources(quote ValueProjection, frame valueFrame, state EntitlementValueState, at time.Time) (map[uint]int64, error) {
	// quote.Sources contains current plus future value. Subtract future source
	// allocations exactly instead of proportionally sharing them with the reset.
	out := map[uint]int64{}
	if err := addValueAmounts(out, quote.Sources); err != nil {
		return nil, err
	}
	cycles, sources, err := frame.cycles(state.ResetPolicy)
	if err != nil {
		return nil, err
	}
	for i, c := range cycles {
		if err := frame.ctx.Err(); err != nil {
			return nil, err
		}
		if at.Before(c.StartAt) {
			for id, amount := range sources[i] {
				if out[id] < amount {
					return nil, ErrValueEvidence
				}
				out[id] -= amount
				if out[id] == 0 {
					delete(out, id)
				}
			}
		}
	}
	var total int64
	for _, amount := range out {
		if total > math.MaxInt64-amount {
			return nil, ErrValueEvidence
		}
		total += amount
	}
	if total != quote.CurrentMinor {
		return nil, ErrValueEvidence
	}
	return out, nil
}

func sameValueReset(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
