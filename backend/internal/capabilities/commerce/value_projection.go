package commerce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

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
	TimeOnlyWhenExhausted bool
	Basis                 RemainingValueBasis
	CycleStart            time.Time
	TotalQuota            int64
	Sources               map[uint]int64
}
type valueFrame struct {
	periods []PaidValuePeriod
	owners  []uint
	anchor  time.Time
	end     time.Time
	basis   *valueBasisEvidence
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
func (f valueFrame) cycles(policy int16) ([]ValueCycle, []map[uint]int64, error) {
	cycles, err := SplitPaidValue(f.periods, f.anchor, policy)
	if err != nil {
		return nil, nil, err
	}
	sources := make([]map[uint]int64, len(cycles))
	for i := range sources {
		sources[i] = map[uint]int64{}
	}
	for n, p := range f.periods {
		if f.owners[n] == 0 {
			return nil, nil, ErrValueEvidence
		}
		duration := exactTimeSpan(p.StartAt, p.EndAt)
		for i, c := range cycles {
			left, right := c.StartAt, c.EndAt
			if left.Before(p.StartAt) {
				left = p.StartAt
			}
			if right.After(p.EndAt) {
				right = p.EndAt
			}
			if !right.After(left) {
				continue
			}
			delta := cumulativeHalfUp(p.AmountMinor, exactTimeSpan(p.StartAt, right), duration) - cumulativeHalfUp(p.AmountMinor, exactTimeSpan(p.StartAt, left), duration)
			if err := addValueAmounts(sources[i], map[uint]int64{f.owners[n]: delta}); err != nil {
				return nil, nil, err
			}
		}
	}
	return cycles, sources, nil
}
func quotaForValue(s EntitlementValueState) (int64, error) {
	if s.FlowUsed < 0 || s.CycleStartUsed < 0 || s.FlowUsed < s.CycleStartUsed || s.FlowTotal < s.CycleStartUsed {
		return 0, ErrValueEvidence
	}
	return s.FlowTotal - s.CycleStartUsed, nil
}
func (f valueFrame) project(s EntitlementValueState, at time.Time) (ValueProjection, error) {
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
		if f.basis != nil && f.basis.CycleStart.Equal(c.StartAt) && f.basis.Basis.EndAt.Equal(c.EndAt) {
			basis = f.basis.Basis
			basisSources = f.basis.Sources
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
			value, err = basis.TimeRemaining(at)
			if err != nil {
				return out, err
			}
		}
		if basis.QuotaBytes > 0 {
			value, err = basis.Remaining(at, s.FlowUsed)
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
func ProjectEntitlementValue(records []EntitlementValueSnapshot, current EntitlementValueState, currency string, at time.Time) (ValueProjection, error) {
	empty := ValueProjection{Sources: map[uint]int64{}}
	if len(records) == 0 || len(records) > 1000 || at.IsZero() {
		return empty, ErrValueEvidence
	}
	records = append([]EntitlementValueSnapshot(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].QuotaEventID < records[j].QuotaEventID })
	root := -1
	for i, s := range records {
		if (s.Mode == "new_term" || s.Mode == "recovery_term" || s.Mode == "replacement_term") && s.After.StartAt.Equal(current.StartAt) {
			root = i
		}
	}
	if root < 0 {
		return empty, ErrValueEvidence
	}
	records = records[root:]
	if entitlements.IsPerpetualEnd(current.EndAt) {
		return projectPerpetualQuotaValue(records, current, currency, at)
	}
	frame := valueFrame{anchor: records[0].After.StartAt, end: records[0].After.EndAt}
	var last EntitlementValueSnapshot
	for i, s := range records {
		if s.Version != 1 || s.OrderID == 0 || s.QuotaEventID == 0 || s.QuotaEventAt.IsZero() || s.Currency != currency || !s.PaymentVerified || s.LegacyTransferUnproven || s.SubscriptionID != records[0].SubscriptionID || s.ActualPaidMinor < 0 || s.CapturedAt.IsZero() {
			return empty, ErrValueEvidence
		}
		if i > 0 && (s.QuotaEventID <= last.QuotaEventID || s.Before == nil || !s.Before.StartAt.Equal(last.After.StartAt) || !s.Before.EndAt.Equal(last.After.EndAt) || s.Before.PlanID != last.After.PlanID || s.Before.PlanSKUID != last.After.PlanSKUID || s.Before.ResetPolicy != last.After.ResetPolicy || s.Before.FlowUsed < last.After.FlowUsed) {
			return empty, ErrValueEvidence
		}
		eventAt := s.QuotaEventAt.UTC()
		if i == 0 {
			if (s.Mode != "new_term" && s.Mode != "recovery_term" && s.Mode != "replacement_term") || !s.TermStartAt.Equal(s.After.StartAt) || !s.TermEndAt.Equal(s.After.EndAt) {
				return empty, ErrValueEvidence
			}
			if err := appendValuePeriods(&frame, s); err != nil {
				return empty, err
			}
			last = s
			continue
		}
		before, err := frame.project(*s.Before, eventAt)
		if err != nil {
			return empty, err
		}
		switch s.Mode {
		case "extended_term":
			if s.Before.FlowUsed != s.After.FlowUsed || s.Before.FlowTotal != s.After.FlowTotal || s.Before.CycleStartUsed != s.After.CycleStartUsed || s.Before.ResetPolicy != s.After.ResetPolicy || !sameValueReset(s.Before.NextResetAt, s.After.NextResetAt) {
				return empty, ErrValueEvidence
			}
			priorFrame := frame
			if !s.TermStartAt.Equal(s.Before.EndAt) || !s.TermEndAt.Equal(s.After.EndAt) || !s.After.EndAt.After(s.Before.EndAt) {
				return empty, ErrValueEvidence
			}
			if err := appendValuePeriods(&frame, s); err != nil {
				return empty, err
			}
			frame.end = s.After.EndAt
			cycles, sources, err := frame.cycles(s.After.ResetPolicy)
			if err != nil {
				return empty, err
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
						return empty, ErrValueEvidence
					}
					shares, err := currentProjectionSources(before, priorFrame, *s.Before, eventAt)
					if err != nil {
						return empty, err
					}
					if err := addValueAmounts(shares, map[uint]int64{s.OrderID: added}); err != nil {
						return empty, err
					}
					totalQuota, err := quotaForValue(s.After)
					if err != nil {
						return empty, err
					}
					remaining := max(int64(0), s.After.FlowTotal-s.After.FlowUsed)
					frame.basis = &valueBasisEvidence{CycleStart: c.StartAt, TotalQuota: totalQuota, Sources: shares, TimeOnlyWhenExhausted: remaining == 0, Basis: RemainingValueBasis{At: eventAt, EndAt: c.EndAt, AmountMinor: before.CurrentMinor + added, UsedAtBasis: s.After.FlowUsed, QuotaBytes: remaining}}
				}
				break
			}
		case "paid_reset":
			if s.AllocationModel != "reset_value_continuity_v1" || !s.Before.EndAt.Equal(s.After.EndAt) || s.Before.NextResetAt == nil || s.After.NextResetAt == nil || !s.Before.NextResetAt.Equal(*s.After.NextResetAt) || s.After.FlowUsed != s.Before.FlowUsed || s.After.CycleStartUsed != s.After.FlowUsed || before.CurrentMinor > math.MaxInt64-s.ActualPaidMinor {
				return empty, ErrValueEvidence
			}
			// Only current-cycle sources survive the reset. Future paid allocations
			// remain separate and are never repriced or converted by the reset.
			shares, err := currentProjectionSources(before, frame, *s.Before, eventAt)
			if err != nil {
				return empty, err
			}
			if err := addValueAmounts(shares, map[uint]int64{s.OrderID: s.ActualPaidMinor}); err != nil {
				return empty, err
			}
			quota, err := quotaForValue(s.After)
			if err != nil {
				return empty, err
			}
			frame.basis = &valueBasisEvidence{CycleStart: before.CurrentCycle.StartAt, TotalQuota: quota, Sources: shares, Basis: RemainingValueBasis{At: eventAt, EndAt: before.CurrentCycle.EndAt, AmountMinor: before.CurrentMinor + s.ActualPaidMinor, UsedAtBasis: s.After.FlowUsed, QuotaBytes: quota}}
		default:
			return empty, ErrValueEvidence
		}
		last = s
	}
	if current.PlanID != last.After.PlanID || current.PlanSKUID != last.After.PlanSKUID || !current.StartAt.Equal(last.After.StartAt) || !current.EndAt.Equal(last.After.EndAt) || current.ResetPolicy != last.After.ResetPolicy || current.FlowUsed < last.After.FlowUsed {
		return empty, ErrValueEvidence
	}
	out, err := frame.project(current, at)
	if err != nil {
		return empty, err
	}
	out.ValuationModel = "remaining_value_basis_v1"
	out.RootOrderID = records[0].OrderID
	return fingerprintValueProjection(records, current, out)
}

func fingerprintValueProjection(records []EntitlementValueSnapshot, current EntitlementValueState, out ValueProjection) (ValueProjection, error) {
	payload, err := json.Marshal(struct {
		Records []EntitlementValueSnapshot
		State   EntitlementValueState
		Quote   ValueProjection
	}{records, current, out})
	if err != nil {
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
