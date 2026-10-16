package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

type trafficBreakdownCacheAdapter struct {
	cache *trafficSnapshotCache[metering.BreakdownSnapshot]
}

func (a trafficBreakdownCacheAdapter) Get(ctx context.Context, key [32]byte, load func() (metering.BreakdownSnapshot, error)) (metering.BreakdownSnapshot, error) {
	return a.cache.get(ctx, key, load)
}

func (h *handlers) trafficBreakdownHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	if admin && !claims.IsAdmin {
		Forbidden(w, "admin required")
		return
	}
	window, err := parseHistoryWindow(r.URL.Query(), 7)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	q := metering.BreakdownQuery{Administrative: admin, Dimension: r.URL.Query().Get("dimension"), From: window.From, To: window.To, Offset: offset, Limit: limit}
	if q.Dimension == "" {
		q.Dimension = "user"
	}
	for _, filter := range []struct {
		name   string
		target *uint
	}{{"user_id", &q.UserID}, {"subscription_id", &q.SubscriptionID}, {"node_id", &q.NodeID}, {"protocol_endpoint_id", &q.ProtocolEndpointID}} {
		*filter.target, err = positiveQueryID(r.URL.Query(), filter.name)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	result, err := h.services.TrafficBreakdown(trafficBreakdownCacheAdapter{&h.trafficBreakdownCache}).Read(r.Context(), claims.UserID, q)
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	OK(w, result)
}
