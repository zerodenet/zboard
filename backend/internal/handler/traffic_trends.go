package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type trafficTrendSnapshot struct {
	Points      []trafficTrendPoint
	RecordCount int64
	AsOf        time.Time
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
	adminRequest := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	if adminRequest && !claims.IsAdmin {
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
	db := h.trafficQueryDB().WithContext(r.Context())
	query := db.Model(&model.TrafficRecord{}).
		Where("record_at >= ? AND record_at < ?", from.UTC(), to.AddDate(0, 0, 1).UTC())

	var facetUserID uint
	if adminRequest {
		facetUserID, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if facetUserID > 0 {
			query = query.Where("user_id = ?", facetUserID)
		}
	} else {
		facetUserID = claims.UserID
		query = query.Where("user_id = ?", claims.UserID)
	}

	for _, filter := range []struct {
		Key    string
		Column string
	}{
		{Key: "subscription_id", Column: "subscription_id"},
		{Key: "node_id", Column: "node_id"},
		{Key: "protocol_endpoint_id", Column: "protocol_endpoint_id"},
	} {
		query, err = applyTrafficTrendIDFilter(query, r.URL.Query(), filter.Key, filter.Column)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}

	snapshot, err := h.trafficTrendsCache.get(r.Context(), trafficSnapshotQueryKey(query, "trends:"+location.String()), func() (trafficTrendSnapshot, error) {
		result := trafficTrendSnapshot{AsOf: time.Now().UTC()}
		rows, err := h.loadTrafficTrendRowsInLocation(query, from, days, location)
		if err != nil {
			return result, err
		}
		result.Points, result.RecordCount = buildTrafficTrendPoints(from, days, rows)
		return result, nil
	})
	if err != nil {
		ServerError(w, err)
		return
	}

	subscriptions := make([]entityReference, 0)
	if facetUserID > 0 && r.URL.Query().Get("include_subscriptions") == "true" {
		var subscriptionRows []subscriptionReferenceRow
		if err := db.Table("subscriptions").
			Select("subscriptions.id AS id, subscriptions.status AS status, plans.name AS plan_name, plan_skus.name AS sku_name").
			Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").
			Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
			Where("subscriptions.user_id = ?", facetUserID).
			Order("subscriptions.created_at DESC, subscriptions.id DESC").
			Scan(&subscriptionRows).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, row := range subscriptionRows {
			displayName := strings.TrimSpace(row.PlanName)
			if displayName == "" {
				displayName = "订阅"
			}
			subscriptions = append(subscriptions, entityReference{ID: row.ID, Kind: "subscription", DisplayName: displayName, Secondary: strings.TrimSpace(row.SKUName), Status: row.Status})
		}
	}

	OK(w, trafficTrendResponse{
		From:                  from.Format("2006-01-02"),
		To:                    to.Format("2006-01-02"),
		Points:                snapshot.Points,
		RecordCount:           snapshot.RecordCount,
		ConnectionSampleCount: 0,
		PeakConnections:       nil,
		Truncated:             false,
		Subscriptions:         subscriptions,
		AsOf:                  snapshot.AsOf,
	})
}
