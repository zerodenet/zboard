package commerce

import "strings"

func BuildLegacySKU(planID uint, req LegacySKURequest) (SKU, error) {
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	req.Name = strings.TrimSpace(req.Name)
	req.SKUType = strings.ToLower(strings.TrimSpace(req.SKUType))
	if req.SKUType == "" {
		req.SKUType = "new"
	}
	req.BillingUnit = strings.ToLower(strings.TrimSpace(req.BillingUnit))
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	fields := make(map[string]string)
	if req.Code == "" {
		fields["code"] = "请输入 SKU 编码。"
	}
	if req.Name == "" {
		fields["name"] = "请输入规格名称。"
	}
	if req.Currency == "" {
		fields["currency"] = "请输入币种。"
	}
	switch req.SKUType {
	case "new", "renewal", "upgrade", "traffic_pack":
	default:
		fields["sku_type"] = "请选择有效的规格类型。"
	}
	switch req.BillingUnit {
	case "day", "month", "year", "once":
		if req.BillingValue <= 0 {
			fields["billing_value"] = "周期数量必须大于 0。"
		}
	default:
		fields["billing_unit"] = "请选择有效的计费单位。"
	}
	if req.PriceCents < 0 {
		fields["price_cents"] = "价格不能小于 0。"
	}
	if req.SKUType == "traffic_pack" {
		if req.TrafficBytes <= 0 {
			fields["grant_traffic_bytes"] = "流量包的附加流量必须大于 0。"
		}
		if req.DeviceLimit != 0 || req.SpeedLimitMbps != 0 {
			fields["entitlements"] = "流量包只能增加流量，不能修改设备数或限速。"
		}
	} else if req.TrafficBytes != 0 || req.DeviceLimit != 0 || req.SpeedLimitMbps != 0 {
		fields["entitlements"] = "周期规格继承商品权益，不能单独配置流量、设备数或限速。"
	}
	if len(fields) > 0 {
		return SKU{}, validationError("销售规格校验失败。", fields)
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	return SKU{
		PlanID: planID, Code: req.Code, Name: req.Name, SKUType: req.SKUType,
		BillingUnit: req.BillingUnit, BillingValue: req.BillingValue,
		PriceCents: req.PriceCents, Currency: req.Currency, TrafficBytes: req.TrafficBytes,
		DeviceLimit: req.DeviceLimit, SpeedLimitMbps: req.SpeedLimitMbps,
		IsActive: isActive, SortOrder: req.SortOrder,
	}, nil
}
