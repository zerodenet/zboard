package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func grantRequestForOrder(order model.Order) entitlements.GrantRequest {
	return entitlements.GrantRequest{UserID: order.UserID, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, OrderType: order.OrderType, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, RenewalEffect: order.RenewalEffect, TrafficBytes: order.TrafficBytes, SpeedLimitMbps: order.SpeedLimitMbps, DeviceLimit: order.DeviceLimit}
}
