package commerce

// Public projection intentionally excludes provider identity, binding/key,
// opaque token, installation revisions and internal intent/lease details.
type DiscountReservationView struct {
	Required     bool   `json:"required"`
	State        string `json:"state"`
	CanPay       bool   `json:"can_pay"`
	CanCancel    bool   `json:"can_cancel"`
	CanReconcile bool   `json:"can_reconcile"`
	Message      string `json:"message"`
}
