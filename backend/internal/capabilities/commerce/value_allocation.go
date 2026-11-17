package commerce

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"math"
	"math/big"
	"time"
)

var ErrValueEvidence = errors.New("subscription value evidence is incomplete or inconsistent")

const maxValuePeriods = 4096

// PaidValuePeriod records actual paid unit prices. Nonuniform source prices are
// accepted explicitly by SplitPaidValue; never silently replaced with averages.
type PaidValuePeriod struct {
	StartAt     time.Time `json:"start_at"`
	EndAt       time.Time `json:"end_at"`
	AmountMinor int64     `json:"amount_minor"`
}
type ValueCycle struct {
	StartAt     time.Time `json:"start_at"`
	EndAt       time.Time `json:"end_at"`
	AmountMinor int64     `json:"amount_minor"`
}

func cumulativeHalfUp(total int64, numerator, denominator *big.Int) int64 {
	if numerator.Sign() <= 0 {
		return 0
	}
	if numerator.Cmp(denominator) >= 0 {
		return total
	}
	product := new(big.Int).Mul(big.NewInt(total), numerator)
	product.Mul(product, big.NewInt(2))
	product.Add(product, denominator)
	return product.Quo(product, new(big.Int).Mul(denominator, big.NewInt(2))).Int64()
}

// Existing quote valuation uses Unix-second boundaries. Preserve that
// granularity while performing products and rounding with arbitrary precision.
func exactTimeSpan(start, end time.Time) *big.Int {
	return new(big.Int).Sub(big.NewInt(end.Unix()), big.NewInt(start.Unix()))
}

// UniformPaidPeriods is used only when the actual purchased policy declares
// uniform paid units. The sum is exact even when cents cannot divide evenly.
func UniformPaidPeriods(start, end time.Time, unit string, count int, total int64) ([]PaidValuePeriod, error) {
	if start.IsZero() || !end.After(start) || total < 0 || count < 1 {
		return nil, ErrValueEvidence
	}
	units := count
	step := "month"
	switch unit {
	case "year":
		if count > maxValuePeriods/12 {
			return nil, ErrValueEvidence
		}
		units = count * 12
	case "month":
	case "day":
		step = "day"
	default:
		return nil, ErrValueEvidence
	}
	if units > maxValuePeriods {
		return nil, ErrValueEvidence
	}
	expected, err := entitlements.AddBillingPeriod(start, unit, count)
	if err != nil || !expected.Equal(end) {
		return nil, ErrValueEvidence
	}
	out := make([]PaidValuePeriod, 0, units)
	previous := int64(0)
	cursor := start.UTC()
	for i := 1; i <= units; i++ {
		cut, err := entitlements.AddBillingPeriod(start.UTC(), step, i)
		if err != nil {
			return nil, err
		}
		cumulative := cumulativeHalfUp(total, big.NewInt(int64(i)), big.NewInt(int64(units)))
		out = append(out, PaidValuePeriod{StartAt: cursor, EndAt: cut, AmountMinor: cumulative - previous})
		cursor = cut
		previous = cumulative
	}
	return out, nil
}

// SplitPaidValue intersects each actual paid unit with actual quota-cycle
// boundaries. Each source unit rounds cumulative proportions, so adjacent
// differences preserve every cent and never invent a thirteenth paid month.
func SplitPaidValue(periods []PaidValuePeriod, anchor time.Time, resetPolicy int16) ([]ValueCycle, error) {
	return splitPaidValueContext(context.Background(), periods, anchor, resetPolicy)
}

func splitPaidValueContext(ctx context.Context, periods []PaidValuePeriod, anchor time.Time, resetPolicy int16) ([]ValueCycle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(periods) == 0 || len(periods) > maxValuePeriods || anchor.IsZero() || resetPolicy < 0 || resetPolicy > 5 {
		return nil, ErrValueEvidence
	}
	start, end := periods[0].StartAt.UTC(), periods[len(periods)-1].EndAt.UTC()
	if anchor.After(start) || start.IsZero() || !end.After(start) {
		return nil, ErrValueEvidence
	}
	var sum int64
	for i, p := range periods {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if p.AmountMinor < 0 || !p.EndAt.After(p.StartAt) || p.EndAt.Unix() <= p.StartAt.Unix() || (i > 0 && !p.StartAt.Equal(periods[i-1].EndAt)) || sum > math.MaxInt64-p.AmountMinor {
			return nil, ErrValueEvidence
		}
		sum += p.AmountMinor
	}
	cycles := []ValueCycle{}
	cursor := start
	for cursor.Before(end) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(cycles) >= maxValuePeriods {
			return nil, ErrValueEvidence
		}
		cut := end
		if next := entitlements.NextTrafficResetAfter(anchor, resetPolicy, cursor); next != nil && next.Before(cut) {
			cut = *next
		}
		if !cut.After(cursor) {
			return nil, ErrValueEvidence
		}
		cycles = append(cycles, ValueCycle{StartAt: cursor, EndAt: cut})
		cursor = cut
	}
	var allocated int64
	if err := walkValueAllocations(ctx, periods, cycles, func(_, i int, delta int64) error {
		if cycles[i].AmountMinor > math.MaxInt64-delta || allocated > math.MaxInt64-delta {
			return ErrValueEvidence
		}
		cycles[i].AmountMinor += delta
		allocated += delta
		return nil
	}); err != nil {
		return nil, err
	}
	if allocated != sum {
		return nil, ErrValueEvidence
	}
	return cycles, nil
}

// Both inputs have already been validated as contiguous ordered partitions.
// Visit only actual intersections, in the original period-then-cycle order.
// Cumulative half-up remains local to each original paid unit; skipping pairs
// with no overlap cannot combine separate rounding decisions or cash sources.
func walkValueAllocations(ctx context.Context, periods []PaidValuePeriod, cycles []ValueCycle, visit func(int, int, int64) error) error {
	firstCycle := 0
	for n, p := range periods {
		if err := ctx.Err(); err != nil {
			return err
		}
		for firstCycle < len(cycles) && !cycles[firstCycle].EndAt.After(p.StartAt) {
			firstCycle++
		}
		duration := exactTimeSpan(p.StartAt, p.EndAt)
		for i := firstCycle; i < len(cycles) && cycles[i].StartAt.Before(p.EndAt); i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			left, right := cycles[i].StartAt, cycles[i].EndAt
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
			if delta < 0 {
				return ErrValueEvidence
			}
			if err := visit(n, i, delta); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemainingValueBasis uses cumulative accounting usage as its monotonic source.
// A paid reset rebases both constraints at the exact reset instant, after the
// old consumed value is settled. Future cycles are not repriced by this basis.
type RemainingValueBasis struct {
	At          time.Time `json:"at"`
	EndAt       time.Time `json:"end_at"`
	AmountMinor int64     `json:"amount_minor"`
	UsedAtBasis int64     `json:"used_at_basis"`
	QuotaBytes  int64     `json:"quota_bytes"`
}

// TimeRemaining is also used when an extending renewal has no remaining
// billable quota. New cash is not retroactively consumed by exhausted old quota;
// no quota is granted and time still elapses against the settled basis.
func (b RemainingValueBasis) TimeRemaining(now time.Time) (int64, error) {
	if b.At.IsZero() || !b.EndAt.After(b.At) || b.EndAt.Unix() <= b.At.Unix() || b.AmountMinor < 0 || now.Before(b.At) {
		return 0, ErrValueEvidence
	}
	if !now.Before(b.EndAt) {
		return 0, nil
	}
	return cumulativeHalfUp(b.AmountMinor, exactTimeSpan(now, b.EndAt), exactTimeSpan(b.At, b.EndAt)), nil
}
func (b RemainingValueBasis) Remaining(now time.Time, cumulativeUsed int64) (int64, error) {
	if b.UsedAtBasis < 0 || b.QuotaBytes <= 0 || cumulativeUsed < b.UsedAtBasis {
		return 0, ErrValueEvidence
	}
	timeValue, err := b.TimeRemaining(now)
	if err != nil {
		return 0, err
	}
	used := cumulativeUsed - b.UsedAtBasis
	flowValue := proportionalValue(b.AmountMinor, b.QuotaBytes-min(used, b.QuotaBytes), b.QuotaBytes)
	return min(timeValue, flowValue), nil
}

func (b RemainingValueBasis) PaidReset(now time.Time, cumulativeUsed, actualPaidMinor, restoredQuota int64) (RemainingValueBasis, int64, error) {
	before, err := b.Remaining(now, cumulativeUsed)
	if err != nil || (!now.Before(b.EndAt) || b.EndAt.Unix() <= now.Unix()) || actualPaidMinor < 0 || restoredQuota <= 0 || before > math.MaxInt64-actualPaidMinor {
		return RemainingValueBasis{}, 0, ErrValueEvidence
	}
	return RemainingValueBasis{At: now.UTC(), EndAt: b.EndAt, AmountMinor: before + actualPaidMinor, UsedAtBasis: cumulativeUsed, QuotaBytes: restoredQuota}, before, nil
}
