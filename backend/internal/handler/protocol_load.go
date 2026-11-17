package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const protocolActivityWindow = 2 * time.Minute

type accountProtocolLoadItem struct {
	ConnectorOnline     bool       `json:"connector_online"`
	ConnectorLastSeenAt *time.Time `json:"connector_last_seen_at,omitempty"`
	LoadFresh           bool       `json:"load_fresh"`
	LoadObservedAt      *time.Time `json:"load_observed_at,omitempty"`
	ProtocolEndpointID  uint       `json:"protocol_endpoint_id"`
	Name                string     `json:"name"`
	Region              string     `json:"region"`
	Protocol            string     `json:"protocol"`
	ActiveUsers         int64      `json:"active_users"`
	ActiveFlows         int64      `json:"active_flows"`
	LastActivityAt      *time.Time `json:"last_activity_at,omitempty"`
}

type accountProtocolLoadResponse struct {
	SampledAt             time.Time                 `json:"sampled_at"`
	ActivityWindowSeconds int64                     `json:"activity_window_seconds"`
	Items                 []accountProtocolLoadItem `json:"items"`
}

// AccountProtocolLoadHandler exposes only aggregate activity for protocol
// endpoints covered by the caller's currently usable subscriptions. It never
// reveals other users, credentials, host details, or traffic records.
func (h *handlers) AccountProtocolLoadHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	now := time.Now().UTC()
	records, err := h.services.NetworkInventory.AuthorizedProtocolEndpoints(r.Context(), claims.UserID, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	endpoints := make([]model.ProtocolEndpoint, 0, len(records))
	for _, record := range records {
		endpoints = append(endpoints, protocolEndpointRecordModel(record))
	}
	usageByEndpoint, err := h.loadProtocolEndpointUsageBatch(endpoints, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	nodesByID, err := h.loadProtocolEndpointNodes(endpoints)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]accountProtocolLoadItem, 0, len(endpoints))
	for _, endpoint := range endpoints {
		node := nodesByID[endpoint.NodeID]
		if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, node); !supported {
			continue
		}
		usage := usageByEndpoint[endpoint.ID]
		items = append(items, accountProtocolLoadItem{
			ProtocolEndpointID:  endpoint.ID,
			ConnectorOnline:     node.ConnectorLastSeenAt != nil && !node.ConnectorLastSeenAt.Before(now.Add(-protocolActivityWindow)),
			ConnectorLastSeenAt: node.ConnectorLastSeenAt,
			LoadFresh:           usage.ObservedAt != nil && !usage.ObservedAt.Before(now.Add(-protocolActivityWindow)),
			LoadObservedAt:      usage.ObservedAt,
			Name:                endpoint.Name,
			Region:              strings.TrimSpace(node.Region),
			Protocol:            endpoint.Protocol,
			ActiveUsers:         usage.ActiveUsers,
			ActiveFlows:         usage.ActiveFlows,
			LastActivityAt:      usage.LastUsedAt,
		})
	}
	OK(w, accountProtocolLoadResponse{
		SampledAt:             now,
		ActivityWindowSeconds: int64(protocolActivityWindow / time.Second),
		Items:                 items,
	})
}
