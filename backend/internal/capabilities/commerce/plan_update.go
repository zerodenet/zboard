package commerce

import (
	"context"
	"strings"
)

type PlanRevisionError struct {
	Current  uint64
	Required bool
}

func (e *PlanRevisionError) Error() string {
	if e.Required {
		return "plan revision required"
	}
	return "plan revision conflict"
}

type PlanUpdateRepository interface {
	Update(context.Context, uint, uint, PlanUpdateRequest) (Plan, error)
}
type PlanUpdate struct{ Repository PlanUpdateRepository }

func (s PlanUpdate) Update(ctx context.Context, actor, id uint, request PlanUpdateRequest) (Plan, error) {
	if actor == 0 {
		return Plan{}, ErrPermission
	}
	if id == 0 {
		return Plan{}, ErrNotFound
	}
	return s.Repository.Update(ctx, actor, id, request)
}
func ApplyPlanUpdate(current Plan, req PlanUpdateRequest) (Plan, []string, error) {
	if req.ExpectedRevision == nil {
		return Plan{}, nil, &PlanRevisionError{Current: current.Revision, Required: true}
	}
	if *req.ExpectedRevision != current.Revision {
		return Plan{}, nil, &PlanRevisionError{Current: current.Revision}
	}
	candidate := current
	fields := []string{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return Plan{}, nil, validationError("商品信息校验失败。", map[string]string{"name": "请输入商品名称。"})
		}
		candidate.Name = name
		fields = append(fields, "name")
	}
	if req.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*req.Slug))
		if slug == "" {
			return Plan{}, nil, validationError("商品信息校验失败。", map[string]string{"slug": "请输入商品 Slug。"})
		}
		candidate.Slug = slug
		fields = append(fields, "slug")
	}
	if req.Summary != nil {
		candidate.Summary = strings.TrimSpace(*req.Summary)
		fields = append(fields, "summary")
	}
	if req.Description != nil {
		candidate.Description = strings.TrimSpace(*req.Description)
		fields = append(fields, "description")
	}
	if req.SortOrder != nil {
		candidate.SortOrder = *req.SortOrder
		fields = append(fields, "sort_order")
	}
	if req.NodeGroupID != nil {
		if *req.NodeGroupID == 0 {
			return Plan{}, nil, validationError("商品信息校验失败。", map[string]string{"node_group_id": "请选择节点组。"})
		}
		candidate.NodeGroupID = *req.NodeGroupID
		fields = append(fields, "node_group_id")
	}
	if req.TrafficBytes != nil {
		if *req.TrafficBytes <= 0 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"traffic_bytes": "流量配额必须大于 0。"})
		}
		candidate.TrafficBytes = *req.TrafficBytes
		fields = append(fields, "traffic_bytes")
	}
	if req.SpeedLimitMbps != nil {
		if *req.SpeedLimitMbps < 0 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"speed_limit_mbps": "速率限制不能小于 0。"})
		}
		candidate.SpeedLimitMbps = *req.SpeedLimitMbps
		fields = append(fields, "speed_limit_mbps")
	}
	if req.MaxActiveSubscriptions != nil {
		if *req.MaxActiveSubscriptions < 0 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"max_active_subscriptions": "最大有效订阅数不能小于 0。"})
		}
		candidate.MaxActiveSubscriptions = *req.MaxActiveSubscriptions
		fields = append(fields, "max_active_subscriptions")
	}
	if req.IsRenewable != nil {
		candidate.IsRenewable = *req.IsRenewable
		fields = append(fields, "is_renewable")
	}
	if req.DeviceLimit != nil {
		if *req.DeviceLimit <= 0 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"device_limit": "设备数必须大于 0。"})
		}
		candidate.DeviceLimit = *req.DeviceLimit
		fields = append(fields, "device_limit")
	}
	if req.FamilyLimit != nil {
		if *req.FamilyLimit < 0 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"family_limit": "家庭共享人数不能小于 0。"})
		}
		candidate.FamilyLimit = *req.FamilyLimit
		fields = append(fields, "family_limit")
	}
	if req.ResetPolicy != nil {
		if *req.ResetPolicy < 0 || *req.ResetPolicy > 5 {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"reset_policy": "请选择有效的流量重置策略。"})
		}
		candidate.ResetPolicy = *req.ResetPolicy
		fields = append(fields, "reset_policy")
	}
	if req.TrafficCalcMode != nil {
		if !(*req.TrafficCalcMode >= 0 && *req.TrafficCalcMode <= 2) {
			return Plan{}, nil, validationError("套餐策略校验失败。", map[string]string{"traffic_calc_mode": "请选择有效的流量计算方式。"})
		}
		candidate.TrafficCalcMode = *req.TrafficCalcMode
		fields = append(fields, "traffic_calc_mode")
	}
	if req.IsActive != nil {
		candidate.IsActive = *req.IsActive
		fields = append(fields, "is_active")
	}
	if len(fields) == 0 {
		return Plan{}, nil, validationError("no valid update fields", nil)
	}

	candidate.Revision = current.Revision + 1
	return candidate, fields, nil
}
func ValidatePlanGroup(candidate Plan, request PlanUpdateRequest, exists, enabled, hasEndpoint bool) error {
	if request.NodeGroupID != nil && !exists {
		return validationError("商品信息校验失败。", map[string]string{"node_group_id": "所选节点组不存在。"})
	}
	if candidate.IsActive && (request.IsActive != nil || request.NodeGroupID != nil) {
		if !exists || !enabled {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品必须选择已启用的节点组。"})
		}
		if !hasEndpoint {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品的节点组至少需要一个已启用协议端点。"})
		}
	}
	return nil
}
