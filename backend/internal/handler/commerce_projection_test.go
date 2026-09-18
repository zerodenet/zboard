package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type planCatalogItem = commerce.PlanCatalog
type planDetailItem = commerce.PlanDetail
type planSKUCountRow = commerce.PlanSKUCounts

func testCommercePlan(plan model.Plan) commerce.Plan {
	return commerce.Plan{ID: plan.ID,
		Name:                   plan.Name,
		Slug:                   plan.Slug,
		Summary:                plan.Summary,
		Description:            plan.Description,
		NodeGroupID:            plan.NodeGroupID,
		TrafficBytes:           plan.TrafficBytes,
		SpeedLimitMbps:         plan.SpeedLimitMbps,
		MaxActiveSubscriptions: plan.MaxActiveSubscriptions,
		IsRenewable:            plan.IsRenewable,
		DeviceLimit:            plan.DeviceLimit,
		FamilyLimit:            plan.FamilyLimit,
		ResetPolicy:            plan.ResetPolicy,
		TrafficCalcMode:        plan.TrafficCalcMode,
		IsActive:               plan.IsActive,
		SortOrder:              plan.SortOrder,
		Revision:               plan.Revision,
		CreatedAt:              plan.CreatedAt,
		UpdatedAt:              plan.UpdatedAt}
}
func testCommerceGroup(plan model.Plan) *commerce.PlanGroupSummary {
	if plan.NodeGroup == nil {
		return nil
	}
	group := plan.NodeGroup
	return &commerce.PlanGroupSummary{ID: group.ID, Name: group.Name, Code: group.Code, IsEnabled: group.IsEnabled}
}
func newPlanSummaryItem(plan model.Plan, counts planSKUCountRow) commerce.PlanSummary {
	return commerce.SummarizePlan(testCommercePlan(plan), testCommerceGroup(plan), counts)
}
func newPlanDetailItem(plan model.Plan, counts planSKUCountRow) commerce.PlanDetail {
	return commerce.DetailPlan(testCommercePlan(plan), testCommerceGroup(plan), counts)
}
func newPlanCatalogItem(plan model.Plan, counts planSKUCountRow, primary *model.PlanSKU) commerce.PlanCatalog {
	var value *commerce.SKU
	if primary != nil {
		sku := commerce.SKU(*primary)
		value = &sku
	}
	return commerce.CatalogPlan(testCommercePlan(plan), testCommerceGroup(plan), counts, value)
}
