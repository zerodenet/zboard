package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	trafficUsageBucketMinute = "minute"
	trafficUsageBucketHour   = "hour"
	trafficUsageBucketDay    = "day"
)

type trafficUsageBucketSpec struct {
	Name       string
	Expression string
}

func parseTrafficUsageBucket(raw string) (trafficUsageBucketSpec, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", trafficUsageBucketMinute:
		return trafficUsageBucketSpec{
			Name:       trafficUsageBucketMinute,
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:%i:00') AS DATETIME)",
		}, nil
	case trafficUsageBucketHour:
		return trafficUsageBucketSpec{
			Name:       trafficUsageBucketHour,
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:00:00') AS DATETIME)",
		}, nil
	case trafficUsageBucketDay:
		return trafficUsageBucketSpec{
			Name:       trafficUsageBucketDay,
			Expression: "CAST(DATE_FORMAT(record_at, '%Y-%m-%d 00:00:00') AS DATETIME)",
		}, nil
	default:
		return trafficUsageBucketSpec{}, fmt.Errorf("bucket must be minute, hour or day")
	}
}

type trafficUsageBucket struct {
	ID                      uint      `json:"id" gorm:"column:id"`
	UserID                  uint      `json:"user_id" gorm:"column:user_id"`
	SubscriptionID          uint      `json:"subscription_id,omitempty" gorm:"column:subscription_id"`
	NodeID                  uint      `json:"node_id" gorm:"column:node_id"`
	RawBytes                int64     `json:"raw_bytes" gorm:"column:raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes" gorm:"column:upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes" gorm:"column:download_bytes"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli" gorm:"column:protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes" gorm:"column:used_bytes"`
	RecordAt                time.Time `json:"record_at" gorm:"column:record_at"`
	RecordCount             int64     `json:"record_count" gorm:"column:record_count"`
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
	if r == nil || r.URL == nil || !wantsPagedList(r) || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "raw") {
		h.TrafficRecordsHandler(w, r)
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

	base := applyHistoryWindow(h.db.Model(&model.TrafficRecord{}), "record_at", window)
	if adminScope {
		if userID, parseErr := positiveQueryID(r.URL.Query(), "user_id"); parseErr != nil {
			BadRequest(w, parseErr.Error())
			return
		} else if userID > 0 {
			base = base.Where("user_id = ?", userID)
		}
	} else {
		base = base.Where("user_id = ?", claims.UserID)
	}
	for _, filter := range []struct {
		key    string
		column string
	}{
		{key: "subscription_id", column: "subscription_id"},
		{key: "node_id", column: "node_id"},
		{key: "protocol_endpoint_id", column: "protocol_endpoint_id"},
	} {
		id, parseErr := positiveQueryID(r.URL.Query(), filter.key)
		if parseErr != nil {
			BadRequest(w, parseErr.Error())
			return
		}
		if id > 0 {
			base = base.Where(filter.column+" = ?", id)
		}
	}

	var aggregates trafficRecordAggregates
	if err := base.Session(&gorm.Session{}).Select(`
		COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
		COALESCE(SUM(used_bytes), 0) AS used_bytes,
		COUNT(DISTINCT user_id) AS user_count,
		COUNT(DISTINCT NULLIF(subscription_id, 0)) AS subscription_count,
		COUNT(DISTINCT node_id) AS node_count,
		COUNT(DISTINCT protocol_endpoint_id) AS protocol_endpoint_count
	`).Scan(&aggregates).Error; err != nil {
		ServerError(w, err)
		return
	}

	grouped := base.Session(&gorm.Session{}).
		Select(`
			MIN(id) AS id,
			user_id,
			COALESCE(subscription_id, 0) AS subscription_id,
			node_id,
			protocol_multiplier_milli,
			COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
			COALESCE(SUM(upload_bytes), 0) AS upload_bytes,
			COALESCE(SUM(download_bytes), 0) AS download_bytes,
			COALESCE(SUM(used_bytes), 0) AS used_bytes,
			` + bucket.Expression + ` AS record_at,
			COUNT(*) AS record_count
		`).
		Group(bucket.Expression + ", user_id, subscription_id, node_id, protocol_multiplier_milli")

	var total int64
	if err := h.db.Table("(?) AS traffic_usage_buckets", grouped).Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}

	bucketQuery := h.db.Table("(?) AS traffic_usage_buckets", grouped)
	if cursor != nil {
		if cursor.Direction == historyDirectionOlder {
			bucketQuery = bucketQuery.Where("(record_at < ?) OR (record_at = ? AND id < ?)", cursor.At, cursor.At, cursor.ID)
		} else {
			bucketQuery = bucketQuery.Where("(record_at > ?) OR (record_at = ? AND id > ?)", cursor.At, cursor.At, cursor.ID)
		}
	}
	order := "record_at desc, id desc"
	if cursor != nil && cursor.Direction == historyDirectionNewer {
		order = "record_at asc, id asc"
	}

	buckets := make([]trafficUsageBucket, 0, limit+1)
	if cursor == nil && offset > 0 {
		if err := bucketQuery.Order("record_at desc, id desc").Offset(offset).Limit(limit).Scan(&buckets).Error; err != nil {
			ServerError(w, err)
			return
		}
		data := pagedData(buckets, total, offset, limit)
		data["aggregates"] = aggregates
		data["bucket"] = bucket.Name
		OK(w, data)
		return
	}
	if err := bucketQuery.Order(order).Limit(limit + 1).Scan(&buckets).Error; err != nil {
		ServerError(w, err)
		return
	}
	hasMore := len(buckets) > limit
	if hasMore {
		buckets = buckets[:limit]
	}
	if cursor != nil && cursor.Direction == historyDirectionNewer {
		reverseHistoryPage(buckets)
	}

	var nextCursor, previousCursor *string
	if len(buckets) > 0 {
		nextCursor, previousCursor, err = historyPageCursorValues(
			historyKey{At: buckets[0].RecordAt, ID: buckets[0].ID},
			historyKey{At: buckets[len(buckets)-1].RecordAt, ID: buckets[len(buckets)-1].ID},
			cursor,
			hasMore,
		)
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	data := cursorPagedData(buckets, total, limit, nextCursor, previousCursor)
	data["aggregates"] = aggregates
	data["bucket"] = bucket.Name
	OK(w, data)
}
