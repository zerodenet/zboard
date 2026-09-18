package commerce

import "time"

const (
	skuBillingPeriodic         = "periodic"
	skuBillingOneTime          = "one_time"
	skuEntitlementPlan         = "plan"
	skuEntitlementTrafficAddon = "traffic_addon"
	skuRenewalNone             = "none"
	skuRenewalExtendOnly       = "extend_only"
	skuRenewalExtendAndAdd     = "extend_and_add_quota"
	skuRenewalAddQuotaOnly     = "add_quota_only"

	skuOperationPurchase = "purchase"
	skuOperationRenew    = "renew"
	skuOperationChange   = "change"
	skuOperationAddon    = "addon"
)

var skuOperationOrder = map[string]int{
	skuOperationPurchase: 0,
	skuOperationRenew:    1,
	skuOperationChange:   2,
	skuOperationAddon:    3,
}

type SKURequest struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	SKUType           string   `json:"sku_type"` // Deprecated compatibility input.
	BillingMode       string   `json:"billing_mode"`
	EntitlementMode   string   `json:"entitlement_mode"`
	RenewalEffect     string   `json:"renewal_effect"`
	AllowedOperations []string `json:"allowed_operations"`
	BillingUnit       string   `json:"billing_unit"`
	BillingValue      int      `json:"billing_value"`
	PriceCents        int64    `json:"price_cents"`
	Currency          string   `json:"currency"`
	GrantTrafficBytes int64    `json:"grant_traffic_bytes"`
	TrafficBytes      int64    `json:"traffic_bytes"`    // Deprecated alias for grant_traffic_bytes.
	DeviceLimit       int      `json:"device_limit"`     // Deprecated compatibility input; must be zero.
	SpeedLimitMbps    int      `json:"speed_limit_mbps"` // Deprecated compatibility input; must be zero.
	IsActive          *bool    `json:"is_active"`
	SortOrder         int      `json:"sort_order"`
}

type LegacySKURequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	SKUType        string `json:"sku_type"`
	BillingUnit    string `json:"billing_unit"`
	BillingValue   int    `json:"billing_value"`
	PriceCents     int64  `json:"price_cents"`
	Currency       string `json:"currency"`
	TrafficBytes   int64  `json:"traffic_bytes"`
	DeviceLimit    int    `json:"device_limit"`
	SpeedLimitMbps int    `json:"speed_limit_mbps"`
	IsActive       *bool  `json:"is_active"`
	SortOrder      int    `json:"sort_order"`
}

type SKU struct {
	ID              uint      `json:"id"`
	PlanID          uint      `json:"plan_id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	SKUType         string    `json:"sku_type"`
	BillingMode     string    `json:"billing_mode"`
	EntitlementMode string    `json:"entitlement_mode"`
	RenewalEffect   string    `json:"renewal_effect"`
	BillingUnit     string    `json:"billing_unit"`
	BillingValue    int       `json:"billing_value"`
	PriceCents      int64     `json:"price_cents"`
	Currency        string    `json:"currency"`
	TrafficBytes    int64     `json:"-"`
	DeviceLimit     int       `json:"-"`
	SpeedLimitMbps  int       `json:"-"`
	IsActive        bool      `json:"is_active"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type NormalizedSKU struct {
	SKU               SKU
	BillingMode       string
	EntitlementMode   string
	RenewalEffect     string
	AllowedOperations []string
}
