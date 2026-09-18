package commerce

import "time"

// OrderListItem is the bounded order projection used by both account and
// administration tables. Payment callback bodies and failure diagnostics are
// detail-only and must never be returned by a list endpoint.
type OrderListItem struct {
	PayableAmount  int64     `json:"payable_amount"`
	ID             uint      `json:"id"`
	UserID         uint      `json:"user_id"`
	SubscriptionID uint      `json:"subscription_id"`
	PlanID         uint      `json:"plan_id"`
	PlanSKUID      uint      `json:"plan_sku_id"`
	TradeNo        string    `json:"trade_no"`
	OrderType      string    `json:"order_type"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	PlanName       string    `json:"plan_name"`
	SKUName        string    `json:"sku_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type OrderDetail struct {
	AssignedBy     uint   `json:"assigned_by"`
	AssignmentNote string `json:"assignment_note"`
	OrderListItem
	TargetSubscriptionID *uint      `json:"target_subscription_id"`
	PaidAmount           int64      `json:"paid_amount"`
	RefundAmount         int64      `json:"refund_amount"`
	DiscountAmount       int64      `json:"discount_amount"`
	Channel              string     `json:"channel"`
	ProviderTradeNo      *string    `json:"provider_trade_no"`
	BillingUnit          string     `json:"billing_unit"`
	BillingValue         int        `json:"billing_value"`
	RenewalEffect        string     `json:"renewal_effect"`
	TrafficBytes         int64      `json:"traffic_bytes"`
	DeviceLimit          int        `json:"device_limit"`
	SpeedLimitMbps       int        `json:"speed_limit_mbps"`
	PaidAt               *time.Time `json:"paid_at"`
	CanceledAt           *time.Time `json:"canceled_at"`
	FulfilledAt          *time.Time `json:"fulfilled_at"`
	RefundedAt           *time.Time `json:"refunded_at"`
	FailureReason        string     `json:"failure_reason"`
}

// PaymentEventSummary intentionally excludes the provider callback
// payload. Operators need the processing timeline and external references,
// not a second raw webhook viewer with uncontrolled encoding or secrets.
type PaymentEventSummary struct {
	ID              uint       `json:"id"`
	Provider        string     `json:"provider"`
	ProviderEventID string     `json:"provider_event_id"`
	EventType       string     `json:"event_type"`
	AmountMinor     int64      `json:"amount_minor"`
	SignatureValid  bool       `json:"signature_valid"`
	ProcessedAt     *time.Time `json:"processed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

func SummarizeOrder(order Order) OrderListItem {
	return OrderListItem{
		PayableAmount: order.PayableAmount,
		ID:            order.ID, UserID: order.UserID, SubscriptionID: order.SubscriptionID,
		PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, TradeNo: order.TradeNo,
		OrderType: order.OrderType, AmountCents: order.AmountCents, Currency: order.Currency,
		Status: order.Status, PlanName: order.PlanName, SKUName: order.SKUName,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

func DetailOrder(order Order) OrderDetail {
	return OrderDetail{
		AssignedBy: order.AssignedBy, AssignmentNote: order.AssignmentNote,
		OrderListItem:        SummarizeOrder(order),
		TargetSubscriptionID: order.TargetSubscriptionID,
		PaidAmount:           order.PaidAmount,
		RefundAmount:         order.RefundAmount,
		DiscountAmount:       order.DiscountAmount,
		Channel:              order.Channel,
		ProviderTradeNo:      order.ProviderTradeNo,
		BillingUnit:          order.BillingUnit,
		BillingValue:         order.BillingValue,
		RenewalEffect:        order.RenewalEffect,
		TrafficBytes:         order.TrafficBytes,
		DeviceLimit:          order.DeviceLimit,
		SpeedLimitMbps:       order.SpeedLimitMbps,
		PaidAt:               order.PaidAt,
		CanceledAt:           order.CanceledAt,
		FulfilledAt:          order.FulfilledAt,
		RefundedAt:           order.RefundedAt,
		FailureReason:        order.FailureReason,
	}
}

func SummarizeOrders(orders []Order) []OrderListItem {
	items := make([]OrderListItem, 0, len(orders))
	for _, order := range orders {
		items = append(items, SummarizeOrder(order))
	}
	return items
}
