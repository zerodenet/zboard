package networkstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

type DeliveryOrder struct{ DB *gorm.DB }

func deliveryAdministrator(tx *gorm.DB, actor uint) (model.User, error) {
	user, err := providerAdmin(tx, actor)
	if errors.Is(err, network.ErrProviderPermission) {
		err = network.ErrDeliveryPermission
	}
	return user, err
}
func (s DeliveryOrder) Read(ctx context.Context, actor uint) (network.DeliverySnapshot, error) {
	var out network.DeliverySnapshot
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := deliveryAdministrator(tx, actor); err != nil {
			return err
		}
		var err error
		out, err = loadDeliveryOrder(tx, false)
		return err
	})
	if err != nil {
		return network.DeliverySnapshot{}, err
	}
	return out, nil
}
func (s DeliveryOrder) Update(ctx context.Context, actor uint, request network.DeliveryOrderRequest) (network.DeliverySnapshot, error) {
	var out network.DeliverySnapshot
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := deliveryAdministrator(tx, actor)
		if err != nil {
			return err
		}
		snapshot, err := loadDeliveryOrder(tx, true)
		if err != nil {
			return err
		}
		if err := network.ValidateDeliveryOrder(snapshot, request); err != nil {
			return err
		}
		byKey := map[string]network.DeliveryItem{}
		for _, item := range snapshot.Items {
			byKey[item.Key] = item
		}
		for index, key := range request.OrderedKeys {
			item := byKey[key]
			var update *gorm.DB
			if item.ServiceKind == "forward" {
				update = tx.Model(&model.NetworkEntry{}).Where("id = ?", item.ID).UpdateColumn("delivery_sort_order", index)
			} else {
				update = tx.Model(&model.ProtocolEndpoint{}).Where("id = ?", item.ID).UpdateColumn("sort_order", index)
			}
			if update.Error != nil {
				return update.Error
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription.delivery.order", Target: "subscription_delivery", Detail: fmt.Sprintf("service_count=%d publish_status=not_required", len(request.OrderedKeys))}).Error; err != nil {
			return err
		}
		out, err = loadDeliveryOrder(tx, true)
		return err
	})
	if err != nil {
		return network.DeliverySnapshot{}, err
	}
	return out, nil
}
func deliveryName(name, fallback string) string {
	if value := strings.TrimSpace(name); value != "" {
		return value
	}
	return fallback
}
func loadDeliveryOrder(db *gorm.DB, lock bool) (network.DeliverySnapshot, error) {
	var endpoints []model.ProtocolEndpoint
	var entries []model.NetworkEntry
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := db.Session(&gorm.Session{}).Select("id", "node_id", "name", "protocol", "is_active", "sort_order").Order("id").Find(&endpoints).Error; err != nil {
		return network.DeliverySnapshot{}, err
	}
	if err := db.Session(&gorm.Session{}).Select("id", "node_id", "endpoint_id", "name", "enabled", "delivery_sort_order").Order("id").Find(&entries).Error; err != nil {
		return network.DeliverySnapshot{}, err
	}
	items := make([]network.DeliveryItem, 0, len(endpoints)+len(entries))
	byID := map[uint]model.ProtocolEndpoint{}
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
		items = append(items, network.DeliveryItem{DeliveryEndpoint: network.DeliveryEndpoint{ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Protocol: endpoint.Protocol, IsActive: endpoint.IsActive, SortOrder: endpoint.SortOrder}, Key: network.DeliveryKey(endpoint.ID, 0), ServiceKind: "listener"})
	}
	for _, entry := range entries {
		endpoint := byID[entry.EndpointID]
		order := endpoint.SortOrder
		if entry.DeliverySortOrder != nil {
			order = *entry.DeliverySortOrder
		}
		items = append(items, network.DeliveryItem{DeliveryEndpoint: network.DeliveryEndpoint{ID: entry.ID, NodeID: entry.NodeID, Name: deliveryName(entry.Name, endpoint.Name), Protocol: endpoint.Protocol, IsActive: entry.Enabled && endpoint.IsActive, SortOrder: order}, Key: network.DeliveryKey(entry.EndpointID, entry.ID), ServiceKind: "forward"})
	}
	return network.ProjectDeliveryOrder(items), nil
}
