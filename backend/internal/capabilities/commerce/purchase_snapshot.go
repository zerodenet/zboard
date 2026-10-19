package commerce

import "time"

// PurchaseSnapshot contains policies missing from the original order columns.
// Version zero/empty is legacy and must never be backfilled from today's plan.
// Access group membership and current capacity remain resource policy, not
// immutable commercial snapshots.
type PurchaseSnapshot struct {
	Version         int                    `json:"version"`
	BasePriceMinor  int64                  `json:"base_price_minor"`
	Currency        string                 `json:"currency"`
	BillingUnit     string                 `json:"billing_unit"`
	BillingValue    int                    `json:"billing_value"`
	RenewalEffect   string                 `json:"renewal_effect"`
	Renewable       bool                   `json:"renewable"`
	ResetPolicy     int16                  `json:"reset_policy"`
	TrafficCalcMode int16                  `json:"traffic_calc_mode"`
	FamilyLimit     int                    `json:"family_limit"`
	RenewalTarget   *RenewalTargetSnapshot `json:"renewal_target,omitempty"`
}

// Business facts only: metering counters and runtime observations cannot make
// an unchanged renewal intent stale.
type RenewalTargetSnapshot struct {
	StartAt         time.Time `json:"start_at"`
	EndAt           time.Time `json:"end_at"`
	PlanSKUID       uint      `json:"plan_sku_id"`
	ResetQuotaBytes int64     `json:"reset_quota_bytes"`
	ResetPolicy     int16     `json:"reset_policy"`
	TrafficCalcMode int16     `json:"traffic_calc_mode"`
	DeviceLimit     int       `json:"device_limit"`
	SpeedLimitMbps  int       `json:"speed_limit_mbps"`
	FamilyLimit     int       `json:"family_limit"`
}
