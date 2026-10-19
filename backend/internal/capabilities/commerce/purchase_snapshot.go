package commerce

// PurchaseSnapshot contains policies missing from the original order columns.
// Version zero/empty is legacy and must never be backfilled from today's plan.
// Access group membership and current capacity remain resource policy, not
// immutable commercial snapshots.
type PurchaseSnapshot struct {
	Version         int    `json:"version"`
	BasePriceMinor  int64  `json:"base_price_minor"`
	Currency        string `json:"currency"`
	BillingUnit     string `json:"billing_unit"`
	BillingValue    int    `json:"billing_value"`
	RenewalEffect   string `json:"renewal_effect"`
	Renewable       bool   `json:"renewable"`
	ResetPolicy     int16  `json:"reset_policy"`
	TrafficCalcMode int16  `json:"traffic_calc_mode"`
	FamilyLimit     int    `json:"family_limit"`
}
