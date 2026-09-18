package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
	"strings"
	"time"
)

type trafficTrendSnapshot = metering.TrafficTrendSnapshot
type trafficTrendCacheAdapter struct {
	cache *trafficSnapshotCache[trafficTrendSnapshot]
}

func (a trafficTrendCacheAdapter) Get(ctx context.Context, key [32]byte, load func() (metering.TrafficTrendSnapshot, error)) (metering.TrafficTrendSnapshot, error) {
	return a.cache.get(ctx, key, load)
}
func (h *handlers) TrafficTrendsHandler(w http.ResponseWriter, r *http.Request) {
	h.trafficTrends(w, r, false)
}
func (h *handlers) trafficTrends(w http.ResponseWriter, r *http.Request, systemCalendar bool) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	if admin && !claims.IsAdmin {
		Forbidden(w, "admin access required")
		return
	}
	location := time.UTC
	if systemCalendar {
		location = h.systemTimezoneLocation()
	}
	from, to, days, err := parseTrafficTrendRangeInLocation(r.URL.Query(), time.Now().UTC(), location)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	q := metering.TrafficTrendQuery{Administrative: admin, From: from, Days: days, Timezone: location.String(), IncludeSubscriptions: r.URL.Query().Get("include_subscriptions") == "true"}
	if admin {
		q.UserID, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	for _, f := range []struct {
		key string
		id  *uint
	}{{"subscription_id", &q.SubscriptionID}, {"node_id", &q.NodeID}, {"protocol_endpoint_id", &q.ProtocolEndpointID}} {
		*f.id, err = positiveQueryID(r.URL.Query(), f.key)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	for _, b := range systemDayBuckets(from, days, location) {
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: b.Key, StartUTC: b.StartUTC, EndUTC: b.EndUTC})
	}
	result, err := h.services.TrafficTrends(trafficTrendCacheAdapter{&h.trafficTrendsCache}).Read(r.Context(), claims.UserID, q)
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	refs := make([]entityReference, 0, len(result.Subscriptions))
	for _, v := range result.Subscriptions {
		refs = append(refs, entityReference{ID: v.ID, Kind: "subscription", DisplayName: v.DisplayName, Secondary: v.Secondary, Status: v.Status})
	}
	OK(w, trafficTrendResponse{From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Points: result.Snapshot.Points, RecordCount: result.Snapshot.RecordCount, Subscriptions: refs, AsOf: result.Snapshot.AsOf})
}
