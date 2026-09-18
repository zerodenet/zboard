package commerce

import (
	"sort"
	"strings"
)

func LegacySKUTypeOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "renewal":
		return skuOperationRenew
	case "upgrade":
		return skuOperationChange
	case "traffic_pack":
		return skuOperationAddon
	default:
		return skuOperationPurchase
	}
}

func CompatibilitySKUType(entitlementMode string, operations []string) string {
	if entitlementMode == skuEntitlementTrafficAddon {
		return "traffic_pack"
	}
	if ContainsOperation(operations, skuOperationPurchase) {
		return "new"
	}
	if ContainsOperation(operations, skuOperationRenew) {
		return "renewal"
	}
	return "upgrade"
}

func NormalizeOperations(values []string, legacyType string) ([]string, error) {
	if len(values) == 0 {
		values = []string{LegacySKUTypeOperation(legacyType)}
	}
	seen := make(map[string]struct{}, len(values))
	operations := make([]string, 0, len(values))
	for _, value := range values {
		operation := strings.ToLower(strings.TrimSpace(value))
		if _, valid := skuOperationOrder[operation]; !valid {
			return nil, validationError("销售规格校验失败。", map[string]string{
				"allowed_operations": "可用场景只能包含新购、续费、套餐切换或附加购买。",
			})
		}
		if _, exists := seen[operation]; exists {
			continue
		}
		seen[operation] = struct{}{}
		operations = append(operations, operation)
	}
	if len(operations) == 0 {
		return nil, validationError("销售规格校验失败。", map[string]string{
			"allowed_operations": "请至少选择一个可用场景。",
		})
	}
	sort.SliceStable(operations, func(left, right int) bool {
		return skuOperationOrder[operations[left]] < skuOperationOrder[operations[right]]
	})
	return operations, nil
}

func NormalizeSKU(planID uint, request SKURequest) (NormalizedSKU, error) {
	billingMode := strings.ToLower(strings.TrimSpace(request.BillingMode))
	if billingMode == "" {
		if strings.EqualFold(strings.TrimSpace(request.BillingUnit), "once") || strings.EqualFold(strings.TrimSpace(request.SKUType), "traffic_pack") {
			billingMode = skuBillingOneTime
		} else {
			billingMode = skuBillingPeriodic
		}
	}
	if billingMode != skuBillingPeriodic && billingMode != skuBillingOneTime {
		return NormalizedSKU{}, validationError("销售规格校验失败。", map[string]string{
			"billing_mode": "计费方式只能是周期计费或一次性计费。",
		})
	}
	operations, err := NormalizeOperations(request.AllowedOperations, request.SKUType)
	if err != nil {
		return NormalizedSKU{}, err
	}
	fields := map[string]string{}
	grantTrafficBytes := request.GrantTrafficBytes
	if grantTrafficBytes == 0 {
		grantTrafficBytes = request.TrafficBytes
	}
	billingUnit := strings.ToLower(strings.TrimSpace(request.BillingUnit))
	entitlementMode := strings.ToLower(strings.TrimSpace(request.EntitlementMode))
	if entitlementMode == "" {
		if strings.EqualFold(strings.TrimSpace(request.SKUType), "traffic_pack") || ContainsOperation(operations, skuOperationAddon) {
			entitlementMode = skuEntitlementTrafficAddon
		} else {
			entitlementMode = skuEntitlementPlan
		}
	}
	if entitlementMode != skuEntitlementPlan && entitlementMode != skuEntitlementTrafficAddon {
		fields["entitlement_mode"] = "权益用途只能是套餐权益或流量加购。"
	}
	renewalEffect := strings.ToLower(strings.TrimSpace(request.RenewalEffect))
	if entitlementMode == skuEntitlementTrafficAddon || !ContainsOperation(operations, skuOperationRenew) {
		renewalEffect = skuRenewalNone
	} else if renewalEffect == "" {
		if billingUnit == "once" {
			renewalEffect = skuRenewalAddQuotaOnly
		} else {
			renewalEffect = skuRenewalExtendOnly
		}
	}
	if entitlementMode == skuEntitlementTrafficAddon {
		if billingMode != skuBillingOneTime {
			fields["billing_mode"] = "流量加购必须使用一次性付费。"
		}
		if billingUnit != "once" {
			fields["billing_unit"] = "流量加购必须使用一次性单位。"
		}
		if len(operations) != 1 || operations[0] != skuOperationAddon {
			fields["allowed_operations"] = "流量加购只能用于附加购买。"
		}
		if grantTrafficBytes <= 0 {
			fields["grant_traffic_bytes"] = "流量包的附加流量必须大于 0。"
		}
		if request.DeviceLimit != 0 || request.SpeedLimitMbps != 0 {
			fields["entitlements"] = "流量包只能增加流量，不能修改设备数或限速。"
		}
	} else if entitlementMode == skuEntitlementPlan {
		if billingMode == skuBillingPeriodic && billingUnit == "once" {
			fields["billing_unit"] = "按周期付费不能使用永久有效；请改为一次性付费。"
		}
		if ContainsOperation(operations, skuOperationAddon) {
			fields["allowed_operations"] = "套餐权益不能用于附加购买；请将权益用途改为流量加购。"
		}
		if ContainsOperation(operations, skuOperationRenew) {
			if billingUnit == "once" && renewalEffect != skuRenewalAddQuotaOnly {
				fields["renewal_effect"] = "永久套餐再次购买只能补充套餐额度。"
			}
			if billingUnit != "once" && renewalEffect != skuRenewalExtendOnly && renewalEffect != skuRenewalExtendAndAdd {
				fields["renewal_effect"] = "限时套餐请选择只延长时间，或延长时间并增加套餐额度。"
			}
		}
		if request.GrantTrafficBytes != 0 || request.TrafficBytes != 0 || request.DeviceLimit != 0 || request.SpeedLimitMbps != 0 {
			fields["entitlements"] = "套餐权益继承商品配置，不能在 SKU 单独覆盖流量、设备数或限速。"
		}
		grantTrafficBytes = 0
	}
	if len(fields) > 0 {
		return NormalizedSKU{}, validationError("销售规格校验失败。", fields)
	}

	legacy := LegacySKURequest{
		Code: request.Code, Name: request.Name,
		SKUType:     CompatibilitySKUType(entitlementMode, operations),
		BillingUnit: request.BillingUnit, BillingValue: request.BillingValue,
		PriceCents: request.PriceCents, Currency: request.Currency,
		TrafficBytes: grantTrafficBytes, DeviceLimit: 0,
		SpeedLimitMbps: 0, IsActive: request.IsActive,
		SortOrder: request.SortOrder,
	}
	sku, err := BuildLegacySKU(planID, legacy)
	if err != nil {
		return NormalizedSKU{}, err
	}
	sku.BillingMode = billingMode
	sku.EntitlementMode = entitlementMode
	sku.RenewalEffect = renewalEffect
	return NormalizedSKU{SKU: sku, BillingMode: billingMode, EntitlementMode: entitlementMode, RenewalEffect: renewalEffect, AllowedOperations: operations}, nil
}

func ContainsOperation(operations []string, target string) bool {
	for _, operation := range operations {
		if operation == target {
			return true
		}
	}
	return false
}

func DefaultRenewalEffect(billingUnit, entitlementMode string, operations []string) string {
	if entitlementMode == skuEntitlementTrafficAddon || !ContainsOperation(operations, skuOperationRenew) {
		return skuRenewalNone
	}
	if billingUnit == "once" {
		return skuRenewalAddQuotaOnly
	}
	return skuRenewalExtendOnly
}

func OperationRank(operation string) (int, bool) {
	rank, ok := skuOperationOrder[operation]
	return rank, ok
}
