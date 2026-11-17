package commerce

import "math"

// AllocateAdministrativeRefund keeps the default residual sources first. A
// lower approval scales those sources with the existing cumulative half-up
// allocator; a higher approval adds only still-funded source budget. This is
// an authorized refund of actual paid money, never an unbacked compensation.
func AllocateAdministrativeRefund(amount int64, remaining, limits map[uint]int64) (map[uint]int64, error) {
	if amount < 0 {
		return nil, ErrSubscriptionSettlementConflict
	}
	var residual, maximum int64
	for id, limit := range limits {
		if id == 0 || limit < 0 || maximum > math.MaxInt64-limit {
			return nil, ErrSubscriptionSettlementConflict
		}
		maximum += limit
	}
	for id, part := range remaining {
		if part < 0 || part > limits[id] || residual > math.MaxInt64-part {
			return nil, ErrSubscriptionSettlementConflict
		}
		residual += part
	}
	if amount > maximum {
		return nil, ErrSubscriptionSettlementConflict
	}
	if amount <= residual {
		return proportionValueSources(amount, remaining)
	}
	out := map[uint]int64{}
	spare := map[uint]int64{}
	for id, limit := range limits {
		out[id] = remaining[id]
		spare[id] = limit - remaining[id]
	}
	extra, err := proportionValueSources(amount-residual, spare)
	if err != nil {
		return nil, err
	}
	if err := addValueAmounts(out, extra); err != nil {
		return nil, err
	}
	return out, nil
}
