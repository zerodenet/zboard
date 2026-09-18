package observabilitystore

import (
	"context"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type EntityReferences struct{ DB *gorm.DB }

func referenceKey(id uint) string { return strconv.FormatUint(uint64(id), 10) }

func (s EntityReferences) Resolve(ctx context.Context, in observability.EntityReferenceRequest, out observability.EntityReferenceData) error {
	db := s.DB.WithContext(ctx)
	steps := []func(*gorm.DB) error{
		func(db *gorm.DB) error { return resolveUsers(db, out.Users, in.Users) },
		func(db *gorm.DB) error { return resolveSubscriptions(db, out.Subscriptions, in.Subscriptions) },
		func(db *gorm.DB) error { return resolveNodes(db, out.Nodes, in.Nodes) },
		func(db *gorm.DB) error {
			return resolveProtocolEndpoints(db, out.ProtocolEndpoints, in.ProtocolEndpoints)
		},
		func(db *gorm.DB) error { return resolvePlans(db, out.Plans, in.Plans) },
		func(db *gorm.DB) error { return resolvePlanSKUs(db, out.PlanSKUs, in.PlanSKUs) },
		func(db *gorm.DB) error { return resolveOrders(db, out.Orders, in.Orders) },
	}
	for _, step := range steps {
		if err := step(db); err != nil {
			return err
		}
	}
	return nil
}

func resolveUsers(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.User
	if err := db.Unscoped().Select("id, account_name, email, status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		display, secondary := strings.TrimSpace(item.AccountName), strings.TrimSpace(item.Email)
		if display == "" {
			display, secondary = secondary, ""
		}
		if display == "" {
			display = "用户"
		}
		result[referenceKey(item.ID)] = observability.EntityReference{ID: item.ID, Kind: "user", DisplayName: display, Secondary: secondary, Status: item.Status}
	}
	return nil
}

type subscriptionReferenceRow struct {
	ID                        uint
	Status, PlanName, SKUName string
}

func resolveSubscriptions(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []subscriptionReferenceRow
	if err := db.Table("subscriptions").Select("subscriptions.id, subscriptions.status, plans.name AS plan_name, plan_skus.name AS sku_name").
		Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
		Where("subscriptions.id IN ?", ids).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		display := strings.TrimSpace(row.PlanName)
		if display == "" {
			display = "订阅"
		}
		result[referenceKey(row.ID)] = observability.EntityReference{ID: row.ID, Kind: "subscription", DisplayName: display, Secondary: strings.TrimSpace(row.SKUName), Status: row.Status}
	}
	return nil
}

func resolveNodes(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Node
	if err := db.Select("id, name, region, lifecycle_status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		display := strings.TrimSpace(item.Name)
		if display == "" {
			display = "节点"
		}
		result[referenceKey(item.ID)] = observability.EntityReference{ID: item.ID, Kind: "node", DisplayName: display, Secondary: strings.TrimSpace(item.Region), Status: item.LifecycleStatus}
	}
	return nil
}

type endpointReferenceRow struct {
	ID                       uint
	Name, Protocol, NodeName string
	IsActive                 bool
}

func resolveProtocolEndpoints(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []endpointReferenceRow
	if err := db.Table("protocol_endpoints").Select("protocol_endpoints.id, protocol_endpoints.name, protocol_endpoints.protocol, protocol_endpoints.is_active, nodes.name AS node_name").
		Joins("LEFT JOIN nodes ON nodes.id = protocol_endpoints.node_id").Where("protocol_endpoints.id IN ?", ids).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		display := strings.TrimSpace(row.Name)
		if display == "" {
			display = strings.TrimSpace(row.NodeName)
		}
		if display == "" {
			display = "协议端点"
		}
		parts := []string{}
		if strings.TrimSpace(row.Protocol) != "" {
			parts = append(parts, strings.TrimSpace(row.Protocol))
		}
		if strings.TrimSpace(row.NodeName) != "" && strings.TrimSpace(row.NodeName) != display {
			parts = append(parts, strings.TrimSpace(row.NodeName))
		}
		status := "inactive"
		if row.IsActive {
			status = "active"
		}
		result[referenceKey(row.ID)] = observability.EntityReference{ID: row.ID, Kind: "protocol_endpoint", DisplayName: display, Secondary: strings.Join(parts, " · "), Status: status}
	}
	return nil
}

func resolvePlans(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Plan
	if err := db.Select("id, name, slug, is_active").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		display := strings.TrimSpace(item.Name)
		if display == "" {
			display = "套餐"
		}
		status := "inactive"
		if item.IsActive {
			status = "active"
		}
		result[referenceKey(item.ID)] = observability.EntityReference{ID: item.ID, Kind: "plan", DisplayName: display, Secondary: strings.TrimSpace(item.Slug), Status: status}
	}
	return nil
}

type planSKUReferenceRow struct {
	ID             uint
	Name, PlanName string
	IsActive       bool
}

func resolvePlanSKUs(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []planSKUReferenceRow
	if err := db.Table("plan_skus").Select("plan_skus.id, plan_skus.name, plan_skus.is_active, plans.name AS plan_name").Joins("LEFT JOIN plans ON plans.id = plan_skus.plan_id").Where("plan_skus.id IN ?", ids).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		display := strings.TrimSpace(row.Name)
		if display == "" {
			display = "套餐规格"
		}
		status := "inactive"
		if row.IsActive {
			status = "active"
		}
		result[referenceKey(row.ID)] = observability.EntityReference{ID: row.ID, Kind: "plan_sku", DisplayName: display, Secondary: strings.TrimSpace(row.PlanName), Status: status}
	}
	return nil
}

func resolveOrders(db *gorm.DB, result map[string]observability.EntityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Order
	if err := db.Select("id, plan_name, sku_name, trade_no, status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		display := strings.TrimSpace(item.PlanName)
		if display == "" {
			display = "订单"
		}
		parts := []string{}
		if strings.TrimSpace(item.SKUName) != "" {
			parts = append(parts, strings.TrimSpace(item.SKUName))
		}
		if strings.TrimSpace(item.TradeNo) != "" {
			parts = append(parts, strings.TrimSpace(item.TradeNo))
		}
		result[referenceKey(item.ID)] = observability.EntityReference{ID: item.ID, Kind: "order", DisplayName: display, Secondary: strings.Join(parts, " · "), Status: item.Status}
	}
	return nil
}
