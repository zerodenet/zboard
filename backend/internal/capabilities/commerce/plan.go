package commerce

import "time"

type PlanUpdateRequest struct {
	Name                   *string `json:"name"`
	Slug                   *string `json:"slug"`
	Summary                *string `json:"summary"`
	Description            *string `json:"description"`
	SortOrder              *int    `json:"sort_order"`
	IsActive               *bool   `json:"is_active"`
	TrafficBytes           *int64  `json:"traffic_bytes"`
	SpeedLimitMbps         *int    `json:"speed_limit_mbps"`
	MaxActiveSubscriptions *int    `json:"max_active_subscriptions"`
	IsRenewable            *bool   `json:"is_renewable"`
	DeviceLimit            *int    `json:"device_limit"`
	FamilyLimit            *int    `json:"family_limit"`
	ResetPolicy            *int16  `json:"reset_policy"`
	TrafficCalcMode        *int16  `json:"traffic_calc_mode"`
	NodeGroupID            *uint   `json:"node_group_id"`
	ExpectedRevision       *uint64 `json:"expected_revision"`
}

type Plan struct {
	ID                     uint      `json:"id"`
	Name                   string    `json:"name"`
	Slug                   string    `json:"slug"`
	Summary                string    `json:"summary"`
	Description            string    `json:"description"`
	NodeGroupID            uint      `json:"node_group_id"`
	TrafficBytes           int64     `json:"traffic_bytes"`
	SpeedLimitMbps         int       `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int       `json:"max_active_subscriptions"`
	IsRenewable            bool      `json:"is_renewable"`
	DeviceLimit            int       `json:"device_limit"`
	FamilyLimit            int       `json:"family_limit"`
	ResetPolicy            int16     `json:"reset_policy"`
	TrafficCalcMode        int16     `json:"traffic_calc_mode"`
	IsActive               bool      `json:"is_active"`
	SortOrder              int       `json:"sort_order"`
	Revision               uint64    `json:"revision"`
	SKUs                   []SKU     `json:"skus,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}
