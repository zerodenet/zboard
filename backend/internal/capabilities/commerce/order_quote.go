package commerce

import (
	"math/big"
	"time"
)

// OrderPreview is a server-owned quote; amounts are currency minor units.
type OrderPreview struct {
	OrderType        string     `json:"order_type"`
	AmountCents      int64      `json:"amount_cents"`
	CreditAmount     int64      `json:"credit_amount"`
	PayableAmount    int64      `json:"payable_amount"`
	Currency         string     `json:"currency"`
	TimeCredit       int64      `json:"time_credit"`
	TrafficCredit    int64      `json:"traffic_credit"`
	TrafficBytes     int64      `json:"traffic_bytes"`
	UsedBytes        int64      `json:"used_bytes"`
	EndAt            *time.Time `json:"end_at,omitempty"`
	QuoteFingerprint string     `json:"quote_fingerprint"`
}

// RemainingCredit values the same paid entitlement along both constraints.
// Rounding happens once per constraint, in minor units, without float arithmetic.
func RemainingCredit(value int64, start, end, now time.Time, quota, used int64) (timeValue, trafficValue int64) {
	if value <= 0 {
		return 0, 0
	}
	if end.After(start) {
		timeValue = proportionalValue(value, end.Unix()-now.Unix(), end.Unix()-start.Unix())
	}
	if quota > 0 {
		trafficValue = proportionalValue(value, quota-used, quota)
	}
	return
}
func proportionalValue(value, remaining, total int64) int64 {
	if remaining <= 0 || total <= 0 {
		return 0
	}
	if remaining >= total {
		return value
	}
	numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(remaining))
	numerator.Add(numerator, big.NewInt(total/2))
	return numerator.Quo(numerator, big.NewInt(total)).Int64()
}
