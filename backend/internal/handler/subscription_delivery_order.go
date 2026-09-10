package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Keys distinguish a forwarding entry from its landing protocol, including
// when their database IDs happen to be equal.
func deliveryOrderKey(endpointID, entryID uint) string {
	if entryID != 0 {
		return fmt.Sprintf("entry:%d", entryID)
	}
	return fmt.Sprintf("protocol:%d", endpointID)
}

type subscriptionDeliveryOrderItem struct {
	protocolEndpointOrderItem
	Key         string `json:"key"`
	ServiceKind string `json:"service_kind"`
}

type subscriptionDeliveryOrderSnapshot struct {
	Items   []subscriptionDeliveryOrderItem `json:"items"`
	Version string                          `json:"version"`
	Total   int                             `json:"total"`
}

func loadSubscriptionDeliveryOrder(db *gorm.DB, lock bool) (subscriptionDeliveryOrderSnapshot, error) {
	var endpoints []model.ProtocolEndpoint
	var entries []model.NetworkEntry
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := db.Session(&gorm.Session{}).Select("id", "node_id", "name", "protocol", "is_active", "sort_order").Order("id").Find(&endpoints).Error; err != nil {
		return subscriptionDeliveryOrderSnapshot{}, err
	}
	if err := db.Session(&gorm.Session{}).Select("id", "node_id", "endpoint_id", "name", "enabled", "delivery_sort_order").Order("id").Find(&entries).Error; err != nil {
		return subscriptionDeliveryOrderSnapshot{}, err
	}
	items := make([]subscriptionDeliveryOrderItem, 0, len(endpoints)+len(entries))
	byID := map[uint]model.ProtocolEndpoint{}
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
		items = append(items, subscriptionDeliveryOrderItem{protocolEndpointOrderItem: protocolEndpointOrderItem{ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Protocol: endpoint.Protocol, IsActive: endpoint.IsActive, SortOrder: endpoint.SortOrder}, Key: deliveryOrderKey(endpoint.ID, 0), ServiceKind: "listener"})
	}
	for _, entry := range entries {
		endpoint := byID[entry.EndpointID]
		order := endpoint.SortOrder
		if entry.DeliverySortOrder != nil {
			order = *entry.DeliverySortOrder
		}
		items = append(items, subscriptionDeliveryOrderItem{protocolEndpointOrderItem: protocolEndpointOrderItem{ID: entry.ID, NodeID: entry.NodeID, Name: networkEntryDisplayName(entry.Name, endpoint.Name), Protocol: endpoint.Protocol, IsActive: entry.Enabled && endpoint.IsActive, SortOrder: order}, Key: deliveryOrderKey(entry.EndpointID, entry.ID), ServiceKind: "forward"})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		if items[i].ServiceKind != items[j].ServiceKind {
			return items[i].ServiceKind == "listener"
		}
		return items[i].ID < items[j].ID
	})
	digest := sha256.New()
	for _, item := range items {
		fmt.Fprintf(digest, "%s:%d;", item.Key, item.SortOrder)
	}
	return subscriptionDeliveryOrderSnapshot{Items: items, Version: hex.EncodeToString(digest.Sum(nil)), Total: len(items)}, nil
}

func (h *handlers) SubscriptionDeliveryOrderHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodGet {
		snapshot, err := loadSubscriptionDeliveryOrder(h.db, false)
		if err != nil {
			ServerError(w, err)
			return
		}
		OK(w, snapshot)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	var req struct {
		OrderedKeys     []string `json:"ordered_keys"`
		ExpectedVersion string   `json:"expected_version"`
	}
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(req.ExpectedVersion) == "" {
		writeJSON(w, http.StatusPreconditionRequired, "请先加载当前订阅展示顺序。", nil)
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		snapshot, err := loadSubscriptionDeliveryOrder(tx, true)
		if err != nil {
			return err
		}
		if snapshot.Version != req.ExpectedVersion {
			return errProtocolEndpointOrderConflict
		}
		available := map[string]subscriptionDeliveryOrderItem{}
		for _, item := range snapshot.Items {
			available[item.Key] = item
		}
		if len(req.OrderedKeys) != len(available) {
			return validationError("订阅展示顺序校验失败。", map[string]string{"ordered_keys": "请提交全部直连服务和前置入口。"})
		}
		for _, key := range req.OrderedKeys {
			if _, ok := available[key]; !ok {
				return validationError("订阅展示顺序校验失败。", map[string]string{"ordered_keys": "列表包含重复或不存在的服务，请重新加载。"})
			}
			delete(available, key)
		}
		byKey := map[string]subscriptionDeliveryOrderItem{}
		for _, item := range snapshot.Items {
			byKey[item.Key] = item
		}
		for index, key := range req.OrderedKeys {
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
		return createAuditLog(tx, claims, "subscription.delivery.order", "subscription_delivery", fmt.Sprintf("service_count=%d publish_status=not_required", len(req.OrderedKeys)))
	})
	if errors.Is(err, errProtocolEndpointOrderConflict) {
		writeJSON(w, http.StatusConflict, "服务列表或展示顺序已更新，请重新加载后保存。", nil)
		return
	}
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
		} else {
			ServerError(w, err)
		}
		return
	}
	snapshot, err := loadSubscriptionDeliveryOrder(h.db, false)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, struct {
		subscriptionDeliveryOrderSnapshot
		Effect        string `json:"effect"`
		PublishStatus string `json:"publish_status"`
	}{snapshot, "delivery", "not_required"})
}
