package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errProtocolEndpointOrderConflict = errors.New("protocol endpoint order conflict")

type protocolEndpointOrderItem struct {
	ID        uint   `json:"id"`
	NodeID    uint   `json:"node_id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
}

type protocolEndpointOrderSnapshot struct {
	Items   []protocolEndpointOrderItem `json:"items"`
	Version string                      `json:"version"`
	Total   int                         `json:"total"`
}

type protocolEndpointOrderRequest struct {
	OrderedIDs      []uint `json:"ordered_ids"`
	ExpectedVersion string `json:"expected_version"`
}

type protocolEndpointOrderMutationResponse struct {
	protocolEndpointOrderSnapshot
	Effect        protocolEndpointEffect `json:"effect"`
	PublishStatus string                 `json:"publish_status"`
}

func (h *handlers) ProtocolEndpointOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.ProtocolEndpointOrderSnapshotHandler(w, r)
		return
	}
	if r.Method == http.MethodPut {
		h.ProtocolEndpointOrderUpdateHandler(w, r)
		return
	}
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
	writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
}

func (h *handlers) ProtocolEndpointOrderSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	snapshot, err := loadProtocolEndpointOrderSnapshot(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, snapshot)
}

func (h *handlers) ProtocolEndpointOrderUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req protocolEndpointOrderRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.ExpectedVersion = strings.TrimSpace(req.ExpectedVersion)
	if req.ExpectedVersion == "" {
		writeJSON(w, http.StatusPreconditionRequired, "调整协议交付顺序前需要提供当前顺序版本。", nil)
		return
	}
	if duplicateID, invalid := duplicateOrZeroUintID(req.OrderedIDs); invalid {
		BadRequestFields(w, "协议交付顺序校验失败。", map[string]string{
			"ordered_ids": fmt.Sprintf("协议服务 ID #%d 无效或重复，请重新加载完整列表。", duplicateID),
		})
		return
	}

	var conflictVersion string
	changed := false
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var endpoints []model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id asc").Find(&endpoints).Error; err != nil {
			return err
		}
		currentVersion := protocolEndpointOrderVersion(endpoints)
		conflictVersion = currentVersion
		if currentVersion != req.ExpectedVersion {
			return errProtocolEndpointOrderConflict
		}
		if err := validateCompleteProtocolEndpointOrder(endpoints, req.OrderedIDs); err != nil {
			return err
		}

		currentOrder := append([]model.ProtocolEndpoint(nil), endpoints...)
		sort.SliceStable(currentOrder, func(left, right int) bool {
			if currentOrder[left].SortOrder != currentOrder[right].SortOrder {
				return currentOrder[left].SortOrder < currentOrder[right].SortOrder
			}
			return currentOrder[left].ID < currentOrder[right].ID
		})
		if len(currentOrder) == len(req.OrderedIDs) {
			changed = false
			for index, endpoint := range currentOrder {
				if endpoint.ID != req.OrderedIDs[index] || endpoint.SortOrder != index {
					changed = true
					break
				}
			}
		}
		if !changed {
			return nil
		}

		caseExpression := strings.Builder{}
		caseExpression.WriteString("CASE id")
		args := make([]interface{}, 0, len(req.OrderedIDs)*2)
		for index, endpointID := range req.OrderedIDs {
			caseExpression.WriteString(" WHEN ? THEN ?")
			args = append(args, endpointID, index)
		}
		caseExpression.WriteString(" END")
		if len(req.OrderedIDs) > 0 {
			if err := tx.Model(&model.ProtocolEndpoint{}).
				Where("id IN ?", req.OrderedIDs).
				UpdateColumn("sort_order", gorm.Expr(caseExpression.String(), args...)).Error; err != nil {
				return err
			}
		}
		return createAuditLog(tx, claims, "protocol_endpoint.order", "protocol_endpoints", fmt.Sprintf("endpoint_count=%d publish_status=%s", len(req.OrderedIDs), protocolEndpointPublishNotRequired))
	})
	if err != nil {
		if errors.Is(err, errProtocolEndpointOrderConflict) {
			writeJSON(w, http.StatusConflict, "协议交付顺序已被其他管理员更新，请重新加载后再保存。", map[string]interface{}{"current_version": conflictVersion})
			return
		}
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		ServerError(w, err)
		return
	}

	snapshot, err := loadProtocolEndpointOrderSnapshot(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, protocolEndpointOrderMutationResponse{
		protocolEndpointOrderSnapshot: snapshot,
		Effect:                        protocolEndpointEffectDelivery,
		PublishStatus:                 protocolEndpointPublishNotRequired,
	})
}

func loadProtocolEndpointOrderSnapshot(db *gorm.DB) (protocolEndpointOrderSnapshot, error) {
	var endpoints []model.ProtocolEndpoint
	query := db.Select("id", "node_id", "name", "protocol", "is_active", "sort_order")
	if err := query.Order("sort_order asc, id asc").Find(&endpoints).Error; err != nil {
		return protocolEndpointOrderSnapshot{}, err
	}
	items := make([]protocolEndpointOrderItem, 0, len(endpoints))
	for _, endpoint := range endpoints {
		items = append(items, protocolEndpointOrderItem{
			ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Protocol: endpoint.Protocol,
			IsActive: endpoint.IsActive, SortOrder: endpoint.SortOrder,
		})
	}
	return protocolEndpointOrderSnapshot{
		Items: items, Version: protocolEndpointOrderVersion(endpoints), Total: len(items),
	}, nil
}

func protocolEndpointOrderVersion(endpoints []model.ProtocolEndpoint) string {
	ordered := append([]model.ProtocolEndpoint(nil), endpoints...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].ID < ordered[right].ID })
	digest := sha256.New()
	for _, endpoint := range ordered {
		_, _ = digest.Write([]byte(strconv.FormatUint(uint64(endpoint.ID), 10)))
		_, _ = digest.Write([]byte(":"))
		_, _ = digest.Write([]byte(strconv.Itoa(endpoint.SortOrder)))
		_, _ = digest.Write([]byte(";"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func validateCompleteProtocolEndpointOrder(endpoints []model.ProtocolEndpoint, orderedIDs []uint) error {
	if len(endpoints) != len(orderedIDs) {
		return validationError("协议交付顺序校验失败。", map[string]string{
			"ordered_ids": fmt.Sprintf("必须提交全部 %d 个协议服务，当前仅收到 %d 个。", len(endpoints), len(orderedIDs)),
		})
	}
	available := make(map[uint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		available[endpoint.ID] = struct{}{}
	}
	for _, endpointID := range orderedIDs {
		if _, exists := available[endpointID]; !exists {
			return validationError("协议交付顺序校验失败。", map[string]string{
				"ordered_ids": fmt.Sprintf("协议服务 #%d 不在当前完整范围内，请重新加载。", endpointID),
			})
		}
	}
	return nil
}

func duplicateOrZeroUintID(values []uint) (uint, bool) {
	seen := make(map[uint]struct{}, len(values))
	for _, value := range values {
		if value == 0 {
			return value, true
		}
		if _, exists := seen[value]; exists {
			return value, true
		}
		seen[value] = struct{}{}
	}
	return 0, false
}

type subscriptionDeliveryRelation struct {
	NodeGroupID        uint
	ProtocolEndpointID uint
	GroupSortOrder     int
	GlobalSortOrder    int
}

type subscriptionDeliveryPosition struct {
	GroupRank    int
	EndpointRank int
	GlobalOrder  int
}

func (h *handlers) sortSubscriptionManifestNodes(subscriptions []model.Subscription, nodes []subscriptionManifestNode) error {
	if len(nodes) < 2 || len(subscriptions) == 0 {
		return nil
	}
	groupIDs := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		groupIDs = append(groupIDs, subscription.NodeGroupID)
	}
	var relations []subscriptionDeliveryRelation
	if err := h.db.Table("node_group_endpoints").
		Select("node_group_endpoints.node_group_id, node_group_endpoints.protocol_endpoint_id, node_group_endpoints.sort_order AS group_sort_order, protocol_endpoints.sort_order AS global_sort_order").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id IN ?", uniqueUintIDs(groupIDs)).
		Find(&relations).Error; err != nil {
		return err
	}
	orderSubscriptionManifestNodes(subscriptions, relations, nodes)
	return nil
}

func orderSubscriptionManifestNodes(subscriptions []model.Subscription, relations []subscriptionDeliveryRelation, nodes []subscriptionManifestNode) {
	subscriptionRank := make(map[uint]int, len(subscriptions))
	subscriptionGroup := make(map[uint]uint, len(subscriptions))
	for index, subscription := range subscriptions {
		subscriptionRank[subscription.ID] = index
		subscriptionGroup[subscription.ID] = subscription.NodeGroupID
	}

	groupRelations := make(map[uint][]subscriptionDeliveryRelation)
	globalOrder := make(map[uint]int)
	for _, relation := range relations {
		groupRelations[relation.NodeGroupID] = append(groupRelations[relation.NodeGroupID], relation)
		globalOrder[relation.ProtocolEndpointID] = relation.GlobalSortOrder
	}
	groupEndpointRank := make(map[uint]map[uint]int, len(groupRelations))
	for groupID, items := range groupRelations {
		hasExplicitOrder := false
		for _, item := range items {
			if item.GroupSortOrder != 0 {
				hasExplicitOrder = true
				break
			}
		}
		sort.SliceStable(items, func(left, right int) bool {
			if hasExplicitOrder && items[left].GroupSortOrder != items[right].GroupSortOrder {
				return items[left].GroupSortOrder < items[right].GroupSortOrder
			}
			if !hasExplicitOrder && items[left].GlobalSortOrder != items[right].GlobalSortOrder {
				return items[left].GlobalSortOrder < items[right].GlobalSortOrder
			}
			return items[left].ProtocolEndpointID < items[right].ProtocolEndpointID
		})
		ranks := make(map[uint]int, len(items))
		for index, item := range items {
			ranks[item.ProtocolEndpointID] = index
		}
		groupEndpointRank[groupID] = ranks
	}

	positionFor := func(node subscriptionManifestNode) subscriptionDeliveryPosition {
		maxRank := int(^uint(0) >> 1)
		position := subscriptionDeliveryPosition{GroupRank: len(subscriptions), EndpointRank: maxRank, GlobalOrder: maxRank}
		if rank, exists := globalOrder[node.ID]; exists {
			position.GlobalOrder = rank
		}
		if node.SubscriptionID != 0 {
			if rank, exists := subscriptionRank[node.SubscriptionID]; exists {
				position.GroupRank = rank
				groupID := subscriptionGroup[node.SubscriptionID]
				if endpointRank, exists := groupEndpointRank[groupID][node.ID]; exists {
					position.EndpointRank = endpointRank
				}
				return position
			}
		}
		for index, subscription := range subscriptions {
			if endpointRank, exists := groupEndpointRank[subscription.NodeGroupID][node.ID]; exists {
				position.GroupRank = index
				position.EndpointRank = endpointRank
				return position
			}
		}
		return position
	}

	sort.SliceStable(nodes, func(left, right int) bool {
		leftPosition := positionFor(nodes[left])
		rightPosition := positionFor(nodes[right])
		if leftPosition.GroupRank != rightPosition.GroupRank {
			return leftPosition.GroupRank < rightPosition.GroupRank
		}
		if leftPosition.EndpointRank != rightPosition.EndpointRank {
			return leftPosition.EndpointRank < rightPosition.EndpointRank
		}
		if leftPosition.GlobalOrder != rightPosition.GlobalOrder {
			return leftPosition.GlobalOrder < rightPosition.GlobalOrder
		}
		if nodes[left].ID != nodes[right].ID {
			return nodes[left].ID < nodes[right].ID
		}
		if nodes[left].SubscriptionID != nodes[right].SubscriptionID {
			return nodes[left].SubscriptionID < nodes[right].SubscriptionID
		}
		return nodes[left].CredentialID < nodes[right].CredentialID
	})
}
