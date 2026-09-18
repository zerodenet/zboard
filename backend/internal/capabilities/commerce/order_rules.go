package commerce

import "strings"

type OrderCreateRequest struct {
	PlanSKUID            uint   `json:"plan_sku_id"`
	OrderType            string `json:"order_type"` // Deprecated assertion; never overrides the derived operation.
	TargetSubscriptionID uint   `json:"target_subscription_id"`
	Channel              string `json:"channel"`
}

// OrderTarget contains only the subscription facts needed to select an operation.
// The caller must resolve it within the buyer's authorized scope.
type OrderTarget struct{ ID, PlanID uint }

// OrderTerms is the immutable commercial snapshot selected before persistence.
// It does not authorize a buyer, reserve capacity, or perform fulfillment.
type OrderTerms struct {
	OrderType, Channel                                      string
	TargetSubscriptionID                                    *uint
	AmountCents, PayableAmount                              int64
	Currency, PlanName, SKUName, BillingUnit, RenewalEffect string
	BillingValue                                            int
	TrafficBytes                                            int64
	DeviceLimit, SpeedLimitMbps                             int
}

func DeriveOrderType(planID uint, entitlementMode string, operations []string, target *OrderTarget) (string, error) {
	if target == nil {
		if entitlementMode != skuEntitlementPlan || !ContainsOperation(operations, skuOperationPurchase) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许新购。"})
		}
		return "new", nil
	}
	if entitlementMode == skuEntitlementTrafficAddon {
		if !ContainsOperation(operations, skuOperationAddon) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许附加购买。"})
		}
		return "traffic_pack", nil
	}
	if target.PlanID == planID {
		if !ContainsOperation(operations, skuOperationRenew) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许用于续费。"})
		}
		return "renewal", nil
	}
	if !ContainsOperation(operations, skuOperationChange) {
		return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许用于套餐切换。"})
	}
	return "upgrade", nil
}

func SnapshotOrderTerms(plan Plan, sku SKU, entitlementMode string, operations []string, target *OrderTarget, request OrderCreateRequest) (OrderTerms, error) {
	orderType, err := DeriveOrderType(plan.ID, entitlementMode, operations, target)
	if err != nil {
		return OrderTerms{}, err
	}
	if orderType == "renewal" && !plan.IsRenewable {
		return OrderTerms{}, validationError("订单创建失败。", map[string]string{"plan_sku_id": "该商品不支持续费。"})
	}
	if assertion := strings.ToLower(strings.TrimSpace(request.OrderType)); assertion != "" && assertion != orderType {
		return OrderTerms{}, validationError("订单创建失败。", map[string]string{"order_type": "订单类型与当前购买操作不一致，请返回套餐详情后重试。"})
	}
	terms := OrderTerms{
		OrderType: orderType, Channel: strings.TrimSpace(request.Channel),
		AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents, Currency: sku.Currency,
		PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit,
		BillingValue: sku.BillingValue, RenewalEffect: sku.RenewalEffect,
		TrafficBytes: plan.TrafficBytes, DeviceLimit: plan.DeviceLimit, SpeedLimitMbps: plan.SpeedLimitMbps,
	}
	if terms.Channel == "" {
		terms.Channel = "manual"
	}
	if target != nil {
		id := target.ID
		terms.TargetSubscriptionID = &id
	}
	if orderType == "traffic_pack" {
		terms.TrafficBytes = sku.TrafficBytes
		terms.DeviceLimit = 0
		terms.SpeedLimitMbps = 0
	}
	return terms, nil
}
