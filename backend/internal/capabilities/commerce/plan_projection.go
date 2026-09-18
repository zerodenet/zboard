package commerce

import "time"

type PlanGroupSummary struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	IsEnabled bool   `json:"is_enabled"`
}

type PlanSummary struct {
	TrafficBytes   int64             `json:"traffic_bytes"`
	ID             uint              `json:"id"`
	Name           string            `json:"name"`
	Slug           string            `json:"slug"`
	Summary        string            `json:"summary"`
	NodeGroupID    uint              `json:"node_group_id"`
	NodeGroup      *PlanGroupSummary `json:"node_group,omitempty"`
	IsActive       bool              `json:"is_active"`
	SortOrder      int               `json:"sort_order"`
	Revision       uint64            `json:"revision"`
	SKUCount       int64             `json:"sku_count"`
	ActiveSKUCount int64             `json:"active_sku_count"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type PlanDetail struct {
	PlanSummary
	Description            string `json:"description"`
	TrafficBytes           int64  `json:"traffic_bytes"`
	SpeedLimitMbps         int    `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int    `json:"max_active_subscriptions"`
	IsRenewable            bool   `json:"is_renewable"`
	DeviceLimit            int    `json:"device_limit"`
	FamilyLimit            int    `json:"family_limit"`
	ResetPolicy            int16  `json:"reset_policy"`
	TrafficCalcMode        int16  `json:"traffic_calc_mode"`
}

type PlanCatalog struct {
	PlanSummary
	Description    string `json:"description"`
	TrafficBytes   int64  `json:"traffic_bytes"`
	SpeedLimitMbps int    `json:"speed_limit_mbps"`
	DeviceLimit    int    `json:"device_limit"`
	PrimarySKU     *SKU   `json:"primary_sku,omitempty"`
}

type PlanSKUCounts struct {
	PlanID         uint
	SKUCount       int64
	ActiveSKUCount int64
}

func planGroupSummary(group *PlanGroupSummary) *PlanGroupSummary {
	if group == nil {
		return nil
	}
	return &PlanGroupSummary{
		ID: group.ID, Name: group.Name, Code: group.Code, IsEnabled: group.IsEnabled,
	}
}

func SummarizePlan(plan Plan, group *PlanGroupSummary, counts PlanSKUCounts) PlanSummary {
	return PlanSummary{
		TrafficBytes: plan.TrafficBytes,
		ID:           plan.ID, Name: plan.Name, Slug: plan.Slug, Summary: plan.Summary,
		NodeGroupID: plan.NodeGroupID, NodeGroup: planGroupSummary(group),
		IsActive: plan.IsActive, SortOrder: plan.SortOrder, Revision: plan.Revision,
		SKUCount: counts.SKUCount, ActiveSKUCount: counts.ActiveSKUCount,
		CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt,
	}
}

func DetailPlan(plan Plan, group *PlanGroupSummary, counts PlanSKUCounts) PlanDetail {
	return PlanDetail{
		PlanSummary:            SummarizePlan(plan, group, counts),
		Description:            plan.Description,
		TrafficBytes:           plan.TrafficBytes,
		SpeedLimitMbps:         plan.SpeedLimitMbps,
		MaxActiveSubscriptions: plan.MaxActiveSubscriptions,
		IsRenewable:            plan.IsRenewable,
		DeviceLimit:            plan.DeviceLimit,
		FamilyLimit:            plan.FamilyLimit,
		ResetPolicy:            plan.ResetPolicy,
		TrafficCalcMode:        plan.TrafficCalcMode,
	}
}

func CatalogPlan(plan Plan, group *PlanGroupSummary, counts PlanSKUCounts, primarySKU *SKU) PlanCatalog {
	return PlanCatalog{
		PlanSummary:    SummarizePlan(plan, group, counts),
		Description:    plan.Description,
		TrafficBytes:   plan.TrafficBytes,
		SpeedLimitMbps: plan.SpeedLimitMbps,
		DeviceLimit:    plan.DeviceLimit,
		PrimarySKU:     primarySKU,
	}
}
