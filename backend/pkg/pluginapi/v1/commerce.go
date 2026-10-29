package pluginv1

import (
	"context"
	"encoding/json"
	"time"
)

const CheckoutPaymentSlot = "account.checkout.payment"
const OrderPaymentSlot = "account.orders.payment"

// The context is host-owned. ActorId is an order-bound opaque authority supplied
// only to the native action; it may be persisted for background reconciliation.
type CheckoutContext struct {
	OrderID   uint   `json:"order_id"`
	TradeNo   string `json:"trade_no"`
	AccountID uint   `json:"account_id"`
	// PayableAmount is the frozen amount this channel may collect. For mixed
	// funding it excludes the reserved wallet contribution; never replace it
	// with a SKU price or the full order total returned by an order-list query.
	PayableAmount     int64  `json:"payable_amount"`
	Currency          string `json:"currency"`
	CurrencyPrecision int    `json:"currency_precision"`
	Status            string `json:"status"`
	ReturnURL         string `json:"return_url"`
}
type CommerceOrdersRequest struct {
	PrincipalID string `json:"principal_id"`
	ID          uint   `json:"id,omitempty"`
	Status      string `json:"status,omitempty"`
	Offset      int    `json:"offset,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}
type PaymentFactRequest struct {
	PrincipalID     string    `json:"principal_id"`
	OrderID         uint      `json:"order_id"`
	ProviderEventID string    `json:"provider_event_id"`
	ProviderTradeNo string    `json:"provider_trade_no"`
	Status          string    `json:"status"`
	AmountMinor     int64     `json:"amount_minor"`
	Currency        string    `json:"currency"`
	OccurredAt      time.Time `json:"occurred_at"`
}
type PaymentFactResult struct {
	OrderID   uint   `json:"order_id"`
	Status    string `json:"status"`
	Fulfilled bool   `json:"fulfilled"`
}

func (c *HostClient) ListOrders(ctx context.Context, request CommerceOrdersRequest, out any) error {
	return c.Call(ctx, CommerceOrdersReadCapability, "commerce.orders.list", request, out)
}
func (c *HostClient) RecordPayment(ctx context.Context, request PaymentFactRequest) (PaymentFactResult, error) {
	var out PaymentFactResult
	err := c.Call(ctx, CommercePaymentsWriteCapability, "commerce.payments.record", request, &out)
	return out, err
}
func (r *PageActionRequest) Checkout() (CheckoutContext, error) {
	var out CheckoutContext
	err := json.Unmarshal(r.GetCheckoutJson(), &out)
	return out, err
}

// CommerceOrder contains only the bounded, owned host projection. Amounts use
// two decimal places in currency minor units; use PayableAmount, not SKU price.
type CommerceOrder struct {
	ID            uint   `json:"id"`
	AccountID     uint   `json:"user_id"`
	TradeNo       string `json:"trade_no"`
	PayableAmount int64  `json:"payable_amount"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
}
type CommerceOrderPage struct {
	Items  []CommerceOrder `json:"items"`
	Total  int64           `json:"total"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
}
