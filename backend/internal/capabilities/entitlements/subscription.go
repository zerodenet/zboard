package entitlements

import "time"

type Subscription struct {
	ID                uint       `json:"id"`
	UserID            uint       `json:"user_id"`
	PlanID            uint       `json:"plan_id"`
	PlanSKUID         uint       `json:"plan_sku_id"`
	NodeGroupID       uint       `json:"node_group_id"`
	SubscriptionType  int16      `json:"subscription_type"`
	StartAt           time.Time  `json:"start_at"`
	EndAt             time.Time  `json:"end_at"`
	Status            string     `json:"status"`
	FlowTotal         int64      `json:"flow_total"`
	FlowUsed          int64      `json:"flow_used"`
	SpeedLimitMbps    int        `json:"speed_limit_mbps"`
	DeviceLimit       int        `json:"device_limit"`
	FamilyLimit       int        `json:"family_limit"`
	RenewalPriceMinor int64      `json:"renewal_price_minor"`
	ResetPolicy       int16      `json:"reset_policy"`
	NextResetAt       *time.Time `json:"next_reset_at"`
	TrafficCalcMode   int16      `json:"traffic_calc_mode"`
	Config            string     `json:"config"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
