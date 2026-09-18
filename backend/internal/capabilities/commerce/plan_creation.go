package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type IdentifierConflict struct{ Fields map[string]string }

func (e *IdentifierConflict) Error() string { return "商品或销售规格的标识已被使用。" }

type NewPlan struct {
	Plan Plan
	SKUs []NormalizedSKU
}
type PlanCreationRepository interface {
	Create(context.Context, uint, NewPlan) (Plan, error)
}
type PlanCreation struct{ Repository PlanCreationRepository }

func (s PlanCreation) Create(ctx context.Context, actor uint, request PlanCreateRequest) (Plan, error) {
	if actor == 0 {
		return Plan{}, ErrPermission
	}
	normalized, err := NormalizePlanCreation(request)
	if err != nil {
		return Plan{}, err
	}
	return s.Repository.Create(ctx, actor, normalized)
}
func NormalizePlanCreation(request PlanCreateRequest) (NewPlan, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Slug = strings.ToLower(strings.TrimSpace(request.Slug))
	fields := map[string]string{}
	if request.Name == "" {
		fields["name"] = "请输入商品名称。"
	}
	if request.Slug == "" {
		fields["slug"] = "请输入商品 Slug。"
	}
	if len(request.SKUs) == 0 {
		fields["skus"] = "请至少配置一个销售规格。"
	}
	if request.NodeGroupID == 0 {
		fields["node_group_id"] = "请选择节点组。"
	}
	if len(fields) > 0 {
		return NewPlan{}, validationError("商品信息校验失败。", fields)
	}
	skus := make([]NormalizedSKU, 0, len(request.SKUs))
	codes := map[string]bool{}
	conflicts := map[string]string{}
	purchasable := 0
	for i, request := range request.SKUs {
		sku, err := NormalizeSKU(0, request)
		if err != nil {
			var invalid *ValidationError
			if errors.As(err, &invalid) {
				prefixed := map[string]string{}
				for key, value := range invalid.Fields {
					prefixed[fmt.Sprintf("skus.%d.%s", i, key)] = value
				}
				return NewPlan{}, validationError(invalid.Message, prefixed)
			}
			return NewPlan{}, err
		}
		if codes[sku.SKU.Code] {
			conflicts[fmt.Sprintf("skus.%d.code", i)] = "该 SKU 编码已被其他销售规格使用，请更换后重试。"
		}
		codes[sku.SKU.Code] = true
		if sku.SKU.IsActive && ContainsOperation(sku.AllowedOperations, "purchase") {
			purchasable++
		}
		skus = append(skus, sku)
	}
	if len(conflicts) > 0 {
		return NewPlan{}, &IdentifierConflict{Fields: conflicts}
	}
	policy, err := NormalizePlanPolicy(request)
	if err != nil {
		return NewPlan{}, err
	}
	if request.IsActive && purchasable == 0 {
		return NewPlan{}, validationError("商品信息校验失败。", map[string]string{"skus": "已发布商品至少需要一个允许新购的可售 SKU。"})
	}
	return NewPlan{Plan: Plan{Name: request.Name, Slug: request.Slug, Summary: strings.TrimSpace(request.Summary), Description: strings.TrimSpace(request.Description), IsActive: request.IsActive, SortOrder: request.SortOrder, Revision: 1, NodeGroupID: request.NodeGroupID, TrafficBytes: policy.TrafficBytes, SpeedLimitMbps: policy.SpeedLimitMbps, MaxActiveSubscriptions: policy.MaxActiveSubscriptions, IsRenewable: policy.IsRenewable, DeviceLimit: policy.DeviceLimit, FamilyLimit: policy.FamilyLimit, ResetPolicy: policy.ResetPolicy, TrafficCalcMode: policy.TrafficCalcMode}, SKUs: skus}, nil
}
