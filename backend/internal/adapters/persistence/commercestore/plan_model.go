package commercestore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func planView(row model.Plan) commerce.Plan {
	out := commerce.Plan{ID: row.ID,
		Name:                   row.Name,
		Slug:                   row.Slug,
		Summary:                row.Summary,
		Description:            row.Description,
		NodeGroupID:            row.NodeGroupID,
		TrafficBytes:           row.TrafficBytes,
		SpeedLimitMbps:         row.SpeedLimitMbps,
		MaxActiveSubscriptions: row.MaxActiveSubscriptions,
		IsRenewable:            row.IsRenewable,
		DeviceLimit:            row.DeviceLimit,
		FamilyLimit:            row.FamilyLimit,
		ResetPolicy:            row.ResetPolicy,
		TrafficCalcMode:        row.TrafficCalcMode,
		IsActive:               row.IsActive,
		SortOrder:              row.SortOrder,
		Revision:               row.Revision,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt}
	for _, sku := range row.SKUs {
		out.SKUs = append(out.SKUs, commerce.SKU(sku))
	}
	return out
}
func planRow(value commerce.Plan) model.Plan {
	return model.Plan{ID: value.ID,
		Name:                   value.Name,
		Slug:                   value.Slug,
		Summary:                value.Summary,
		Description:            value.Description,
		NodeGroupID:            value.NodeGroupID,
		TrafficBytes:           value.TrafficBytes,
		SpeedLimitMbps:         value.SpeedLimitMbps,
		MaxActiveSubscriptions: value.MaxActiveSubscriptions,
		IsRenewable:            value.IsRenewable,
		DeviceLimit:            value.DeviceLimit,
		FamilyLimit:            value.FamilyLimit,
		ResetPolicy:            value.ResetPolicy,
		TrafficCalcMode:        value.TrafficCalcMode,
		IsActive:               value.IsActive,
		SortOrder:              value.SortOrder,
		Revision:               value.Revision,
		CreatedAt:              value.CreatedAt,
		UpdatedAt:              value.UpdatedAt}
}
