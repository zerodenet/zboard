package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	trafficUsageBucketMinute = "minute"
	trafficUsageBucketHour   = "hour"
	trafficUsageBucketDay    = "day"
)

type trafficUsageBucketSpec meteringstore.UsageBucketSpec

func parseTrafficUsageBucket(raw string) (trafficUsageBucketSpec, error) {
	b, err := meteringstore.ParseUsageBucket(raw)
	return trafficUsageBucketSpec(b), err
}

type trafficUsageBucket struct {
	ID                      uint              `json:"id" gorm:"column:id"`
	UserID                  uint              `json:"user_id" gorm:"column:user_id"`
	SubscriptionID          uint              `json:"subscription_id,omitempty" gorm:"column:subscription_id"`
	NodeID                  uint              `json:"node_id" gorm:"column:node_id"`
	RawBytes                int64             `json:"raw_bytes" gorm:"column:raw_bytes"`
	UploadBytes             int64             `json:"upload_bytes" gorm:"column:upload_bytes"`
	DownloadBytes           int64             `json:"download_bytes" gorm:"column:download_bytes"`
	ProtocolMultiplierMilli int64             `json:"protocol_multiplier_milli" gorm:"column:protocol_multiplier_milli"`
	UsedBytes               int64             `json:"used_bytes" gorm:"column:used_bytes"`
	RecordAt                trafficBucketTime `json:"record_at" gorm:"column:record_at"`
	RecordCount             int64             `json:"record_count" gorm:"column:record_count"`
}

// TrafficUsageRecordsHandler is the human-facing read model for traffic history.
//
// Raw TrafficRecord rows remain the source of truth for accounting and auditing.
// Paged list requests are collapsed into minute/hour/day buckets while preserving
// the business dimensions that explain charged usage: subscription, node and
// multiplier (plus user in the administrative scope). Protocol endpoints remain
// available as a pre-aggregation filter and through ?view=raw, but they are not
// an aggregation dimension because multiple endpoints on one node with the same
// multiplier describe the same user-facing usage slice.
func (h *handlers) TrafficUsageRecordsHandler(w http.ResponseWriter, r *http.Request) {
	if r != nil && r.URL != nil && strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "node_series") {
		h.trafficNodeSeriesHandler(w, r)
		return
	}
	summaryOnly := r != nil && r.URL != nil && r.URL.Query().Get("view") == "usage_summary"
	if r == nil || r.URL == nil || (!wantsPagedList(r) && !summaryOnly) || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "raw") {
		h.trafficRecordsHandler(w, r)
		return
	}

	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	if adminScope && !claims.IsAdmin {
		Forbidden(w, "admin required")
		return
	}

	bucket, err := parseTrafficUsageBucket(r.URL.Query().Get("bucket"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	includeTotals := true
	if raw := r.URL.Query().Get("include_totals"); raw != "" {
		includeTotals, err = strconv.ParseBool(raw)
		if err != nil {
			BadRequest(w, "invalid include_totals")
			return
		}
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
	cursor, err := decodeHistoryCursor(r.URL.Query().Get("cursor"), nil)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	q := metering.UsageQuery{RecordsQuery: metering.RecordsQuery{Administrative: adminScope, Offset: offset, Limit: limit, From: window.From, To: window.To}, Bucket: bucket.Name, IncludeTotals: includeTotals, SummaryOnly: summaryOnly}
	if cursor != nil {
		q.Cursor = &metering.RecordCursor{At: cursor.At, ID: cursor.ID, Direction: cursor.Direction}
	}
	if adminScope {
		q.UserID, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	for _, f := range []struct {
		name string
		dest *uint
	}{{"subscription_id", &q.SubscriptionID}, {"node_id", &q.NodeID}, {"protocol_endpoint_id", &q.ProtocolEndpointID}} {
		*f.dest, err = positiveQueryID(r.URL.Query(), f.name)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	result, err := h.services.Usage(trafficStatisticsCacheAdapter{&h.trafficStatisticsCache}, h.trafficIncrementalStats).Read(r.Context(), claims.UserID, q)
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	if summaryOnly {
		OK(w, result.Statistics)
		return
	}
	rows := make([]trafficUsageBucket, 0, len(result.Rows))
	for _, v := range result.Rows {
		rows = append(rows, trafficUsageBucket{ID: v.ID, UserID: v.UserID, SubscriptionID: v.SubscriptionID, NodeID: v.NodeID, RawBytes: v.RawBytes, UploadBytes: v.UploadBytes, DownloadBytes: v.DownloadBytes, ProtocolMultiplierMilli: v.ProtocolMultiplierMilli, UsedBytes: v.UsedBytes, RecordAt: trafficBucketTime{Time: v.RecordAt}, RecordCount: v.RecordCount})
	}
	var total *int64
	var aggregates *trafficRecordAggregates
	var asOf *time.Time
	if result.Statistics != nil {
		total = &result.Statistics.Total
		aggregates = &result.Statistics.Aggregates
		asOf = &result.Statistics.AsOf
	}
	var next, previous *string
	pageOffset := 0
	if cursor == nil && offset > 0 {
		pageOffset = offset
	} else if len(rows) > 0 {
		next, previous, err = historyPageCursorValues(historyKey{At: rows[0].RecordAt.Time, ID: rows[0].ID}, historyKey{At: rows[len(rows)-1].RecordAt.Time, ID: rows[len(rows)-1].ID}, cursor, result.HasMore)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	data := trafficUsagePageData(rows, total, pageOffset, limit, next, previous)
	data["aggregates"] = aggregates
	data["statistics_as_of"] = asOf
	data["bucket"] = bucket.Name
	if !adminScope {
		refs := trafficPageReferences{Subscriptions: map[string]entityReference{}, Nodes: map[string]entityReference{}}
		for _, v := range result.References {
			ref := missingEntityReference(v.Kind, v.ID)
			if !v.Missing {
				name := v.Name
				if name == "" {
					name = entityKindLabel(v.Kind)
				}
				ref = entityReference{ID: v.ID, Kind: v.Kind, DisplayName: name, Secondary: v.Secondary, Status: v.Status}
			}
			if v.Kind == "subscription" {
				refs.Subscriptions[entityKey(v.ID)] = ref
			} else {
				refs.Nodes[entityKey(v.ID)] = ref
			}
		}
		data["facets"] = refs
	}
	OK(w, data)
}
