package commerce

type PlanCreateRequest struct {
	Name                   string       `json:"name"`
	Slug                   string       `json:"slug"`
	Summary                string       `json:"summary"`
	Description            string       `json:"description"`
	SortOrder              int          `json:"sort_order"`
	IsActive               bool         `json:"is_active"`
	SKUs                   []SKURequest `json:"skus"`
	NodeGroupID            uint         `json:"node_group_id"`
	TrafficBytes           int64        `json:"traffic_bytes"`
	SpeedLimitMbps         int          `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int          `json:"max_active_subscriptions"`
	IsRenewable            *bool        `json:"is_renewable"`
	DeviceLimit            int          `json:"device_limit"`
	FamilyLimit            int          `json:"family_limit"`
	ResetPolicy            int16        `json:"reset_policy"`
	TrafficCalcMode        int16        `json:"traffic_calc_mode"`
}

type PlanPolicy struct {
	TrafficBytes           int64
	SpeedLimitMbps         int
	MaxActiveSubscriptions int
	IsRenewable            bool
	DeviceLimit            int
	FamilyLimit            int
	ResetPolicy            int16
	TrafficCalcMode        int16
}

func NormalizePlanPolicy(req PlanCreateRequest) (PlanPolicy, error) {
	policy := PlanPolicy{
		TrafficBytes: req.TrafficBytes, SpeedLimitMbps: req.SpeedLimitMbps,
		MaxActiveSubscriptions: req.MaxActiveSubscriptions, DeviceLimit: req.DeviceLimit,
		FamilyLimit: req.FamilyLimit, ResetPolicy: req.ResetPolicy,
		TrafficCalcMode: req.TrafficCalcMode,
		IsRenewable:     true,
	}
	if req.IsRenewable != nil {
		policy.IsRenewable = *req.IsRenewable
	}
	fields := make(map[string]string)
	if policy.TrafficBytes <= 0 {
		fields["traffic_bytes"] = "流量配额必须大于 0。"
	}
	if policy.DeviceLimit <= 0 {
		fields["device_limit"] = "设备数必须大于 0。"
	}
	if policy.SpeedLimitMbps < 0 {
		fields["speed_limit_mbps"] = "速率限制不能小于 0。"
	}
	if policy.MaxActiveSubscriptions < 0 {
		fields["max_active_subscriptions"] = "最大有效订阅数不能小于 0。"
	}
	if policy.FamilyLimit < 0 {
		fields["family_limit"] = "家庭共享人数不能小于 0。"
	}
	if policy.ResetPolicy < 0 || policy.ResetPolicy > 5 {
		fields["reset_policy"] = "请选择有效的流量重置策略。"
	}
	if policy.TrafficCalcMode < 0 || policy.TrafficCalcMode > 2 {
		fields["traffic_calc_mode"] = "请选择有效的流量计算方式。"
	}
	if len(fields) > 0 {
		return PlanPolicy{}, validationError("套餐策略校验失败。", fields)
	}
	return policy, nil
}
