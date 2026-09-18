package handler

import (
	"context"
	"errors"
	"net/http"
	"sort"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type protocolEndpointOrderItem = networkcap.ProtocolEndpointOrderItem
type protocolEndpointOrderSnapshot = networkcap.ProtocolEndpointOrderSnapshot
type protocolEndpointOrderRequest = networkcap.ProtocolEndpointOrderRequest

type protocolEndpointOrderMutationResponse struct {
	protocolEndpointOrderSnapshot
	Effect        protocolEndpointEffect `json:"effect"`
	PublishStatus string                 `json:"publish_status"`
}

func (h *handlers) ProtocolEndpointOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.protocolEndpointOrderSnapshotHandler(w, r)
		return
	}
	if r.Method == http.MethodPut {
		h.protocolEndpointOrderUpdateHandler(w, r)
		return
	}
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
	writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
}

func (h *handlers) protocolEndpointOrderSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	snapshot, err := h.services.ProtocolEndpointOrder.Read(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, networkcap.ErrProtocolEndpointOrderPermission) {
			Forbidden(w, err.Error())
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, snapshot)
}

func (h *handlers) protocolEndpointOrderUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req protocolEndpointOrderRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	snapshot, _, err := h.services.ProtocolEndpointOrder.Update(r.Context(), claims.UserID, req)
	if err != nil {
		if errors.Is(err, networkcap.ErrProtocolEndpointOrderVersionRequired) {
			writeJSON(w, http.StatusPreconditionRequired, "调整协议交付顺序前需要提供当前顺序版本。", nil)
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointOrderConflict) {
			writeJSON(w, http.StatusConflict, "协议交付顺序已被其他管理员更新，请重新加载后再保存。", map[string]interface{}{"current_version": snapshot.Version})
			return
		}
		if errors.Is(err, networkcap.ErrProtocolEndpointOrderPermission) {
			Forbidden(w, err.Error())
			return
		}
		var validation *networkcap.ProtocolEndpointOrderValidation
		if errors.As(err, &validation) {
			BadRequestFields(w, validation.Error(), validation.Fields)
			return
		}
		ServerError(w, err)
		return
	}

	OK(w, protocolEndpointOrderMutationResponse{
		protocolEndpointOrderSnapshot: snapshot,
		Effect:                        protocolEndpointEffectDelivery,
		PublishStatus:                 protocolEndpointPublishNotRequired,
	})
}

type subscriptionDeliveryRelation = networkcap.SubscriptionDeliveryRelation

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
	relations, err := h.services.NetworkInventory.DeliveryRelations(context.Background(), uniqueUintIDs(groupIDs))
	if err != nil {
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
	globalOrder := make(map[string]int)
	for _, relation := range relations {
		groupRelations[relation.NodeGroupID] = append(groupRelations[relation.NodeGroupID], relation)
		globalOrder[deliveryOrderKey(relation.ProtocolEndpointID, relation.NetworkEntryID)] = relation.GlobalSortOrder
	}
	groupEndpointRank := make(map[uint]map[string]int, len(groupRelations))
	for groupID, items := range groupRelations {
		sort.SliceStable(items, func(left, right int) bool {
			if items[left].GlobalSortOrder != items[right].GlobalSortOrder {
				return items[left].GlobalSortOrder < items[right].GlobalSortOrder
			}
			if items[left].GroupSortOrder != items[right].GroupSortOrder {
				return items[left].GroupSortOrder < items[right].GroupSortOrder
			}
			return items[left].ProtocolEndpointID < items[right].ProtocolEndpointID
		})
		ranks := make(map[string]int, len(items))
		for index, item := range items {
			ranks[deliveryOrderKey(item.ProtocolEndpointID, item.NetworkEntryID)] = index
		}
		groupEndpointRank[groupID] = ranks
	}

	positionFor := func(node subscriptionManifestNode) subscriptionDeliveryPosition {
		maxRank := int(^uint(0) >> 1)
		position := subscriptionDeliveryPosition{GroupRank: len(subscriptions), EndpointRank: maxRank, GlobalOrder: maxRank}
		if rank, exists := globalOrder[deliveryOrderKey(node.ID, node.NetworkEntryID)]; exists {
			position.GlobalOrder = rank
		}
		if node.SubscriptionID != 0 {
			if rank, exists := subscriptionRank[node.SubscriptionID]; exists {
				position.GroupRank = rank
				groupID := subscriptionGroup[node.SubscriptionID]
				if endpointRank, exists := groupEndpointRank[groupID][deliveryOrderKey(node.ID, node.NetworkEntryID)]; exists {
					position.EndpointRank = endpointRank
				}
				return position
			}
		}
		for index, subscription := range subscriptions {
			if endpointRank, exists := groupEndpointRank[subscription.NodeGroupID][deliveryOrderKey(node.ID, node.NetworkEntryID)]; exists {
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
		if leftPosition.GlobalOrder != rightPosition.GlobalOrder {
			return leftPosition.GlobalOrder < rightPosition.GlobalOrder
		}
		if nodes[left].NetworkEntryID != nodes[right].NetworkEntryID {
			return nodes[left].NetworkEntryID < nodes[right].NetworkEntryID
		}
		if nodes[left].ID != nodes[right].ID {
			return nodes[left].ID < nodes[right].ID
		}
		if leftPosition.GroupRank != rightPosition.GroupRank {
			return leftPosition.GroupRank < rightPosition.GroupRank
		}
		if leftPosition.EndpointRank != rightPosition.EndpointRank {
			return leftPosition.EndpointRank < rightPosition.EndpointRank
		}
		if nodes[left].SubscriptionID != nodes[right].SubscriptionID {
			return nodes[left].SubscriptionID < nodes[right].SubscriptionID
		}
		return nodes[left].CredentialID < nodes[right].CredentialID
	})
}
