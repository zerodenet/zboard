package commerce

import "time"

type Order struct {
	AssignedBy            uint       `json:"-"`
	AssignmentNote        string     `json:"-"`
	AssignmentFingerprint string     `json:"-"`
	ID                    uint       `json:"id"`
	UserID                uint       `json:"user_id"`
	SubscriptionID        uint       `json:"subscription_id"`
	PlanID                uint       `json:"plan_id"`
	PlanSKUID             uint       `json:"plan_sku_id"`
	TradeNo               string     `json:"trade_no"`
	OrderType             string     `json:"order_type"`
	TargetSubscriptionID  *uint      `json:"target_subscription_id"`
	AmountCents           int64      `json:"amount_cents"`
	PayableAmount         int64      `json:"payable_amount"`
	PaidAmount            int64      `json:"paid_amount"`
	RefundAmount          int64      `json:"refund_amount"`
	DiscountAmount        int64      `json:"discount_amount"`
	Currency              string     `json:"currency"`
	Channel               string     `json:"channel"`
	ProviderTradeNo       *string    `json:"provider_trade_no"`
	Status                string     `json:"status"`
	PlanName              string     `json:"plan_name"`
	SKUName               string     `json:"sku_name"`
	BillingUnit           string     `json:"billing_unit"`
	BillingValue          int        `json:"billing_value"`
	RenewalEffect         string     `json:"renewal_effect"`
	TrafficBytes          int64      `json:"traffic_bytes"`
	DeviceLimit           int        `json:"device_limit"`
	SpeedLimitMbps        int        `json:"speed_limit_mbps"`
	RawCallback           string     `json:"raw_callback"`
	PaidAt                *time.Time `json:"paid_at"`
	CanceledAt            *time.Time `json:"canceled_at"`
	FulfilledAt           *time.Time `json:"fulfilled_at"`
	RefundedAt            *time.Time `json:"refunded_at"`
	FailureReason         string     `json:"failure_reason"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}
