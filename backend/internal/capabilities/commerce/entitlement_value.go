package commerce

import "time"

// EntitlementValueSnapshot is captured during actual fulfillment, never filled
// into an old paid order from today's catalog or current entitlement state.
// PaymentVerified distinguishes trusted cash/wallet facts from administrator
// grants whose actual cash source still requires reconciliation.
type EntitlementValueSnapshot struct {
	QuotaEventID    uint                   `json:"quota_event_id"`
	QuotaEventAt    time.Time              `json:"quota_event_at"`
	PaidPeriods     []PaidValuePeriod      `json:"paid_periods,omitempty"`
	Version         int                    `json:"version"`
	OrderID         uint                   `json:"order_id"`
	SubscriptionID  uint                   `json:"subscription_id"`
	Currency        string                 `json:"currency"`
	ActualPaidMinor int64                  `json:"actual_paid_minor"`
	PaymentVerified bool                   `json:"payment_verified"`
	PaymentBasis    string                 `json:"payment_basis"`
	Kind            string                 `json:"kind"`
	Mode            string                 `json:"mode"`
	CapturedAt      time.Time              `json:"captured_at"`
	TermStartAt     time.Time              `json:"term_start_at"`
	TermEndAt       time.Time              `json:"term_end_at"`
	BillingUnit     string                 `json:"billing_unit"`
	BillingValue    int                    `json:"billing_value"`
	AllocationModel string                 `json:"allocation_model"`
	Before          *EntitlementValueState `json:"before,omitempty"`
	After           EntitlementValueState  `json:"after"`
	// Existing change snapshots combine discount and old credit without proving
	// cash ancestry. Record their presence; never label that credit paid cash.
	LegacyTransferUnproven bool `json:"legacy_transfer_unproven"`
}
type EntitlementValueState struct {
	PlanID          uint       `json:"plan_id"`
	PlanSKUID       uint       `json:"plan_sku_id"`
	StartAt         time.Time  `json:"start_at"`
	EndAt           time.Time  `json:"end_at"`
	Status          string     `json:"status"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	EndReason       string     `json:"end_reason,omitempty"`
	FlowTotal       int64      `json:"flow_total"`
	FlowUsed        int64      `json:"flow_used"`
	CycleStartUsed  int64      `json:"cycle_start_used"`
	ResetQuotaBytes int64      `json:"reset_quota_bytes"`
	ResetPolicy     int16      `json:"reset_policy"`
	NextResetAt     *time.Time `json:"next_reset_at,omitempty"`
}
