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
	trafficNodeSeriesNodeLimit             = 8
	trafficNodeSeriesMinuteMaxDays         = 1
	trafficNodeSeriesFilteredMinuteMaxDays = 7
	trafficNodeSeriesHourMaxDays           = 31
	trafficNodeSeriesFilteredHourMaxDays   = historyMaxWindowDays
)

type trafficNodeSeriesPoint struct {
	RecordAt      time.Time `json:"record_at" gorm:"column:record_at"`
	NodeID        uint      `json:"node_id" gorm:"column:node_id"`
	RawBytes      int64     `json:"raw_bytes" gorm:"column:raw_bytes"`
	UploadBytes   int64     `json:"upload_bytes" gorm:"column:upload_bytes"`
	DownloadBytes int64     `json:"download_bytes" gorm:"column:download_bytes"`
	UsedBytes     int64     `json:"used_bytes" gorm:"column:used_bytes"`
	RecordCount   int64     `json:"record_count" gorm:"column:record_count"`
}

type trafficNodeSeriesResponse struct {
	Bucket    string                   `json:"bucket"`
	From      time.Time                `json:"from"`
	To        time.Time                `json:"to"`
	Points    []trafficNodeSeriesPoint `json:"points"`
	Nodes     []entityReference        `json:"nodes"`
	Truncated bool                     `json:"truncated"`
	NodeLimit int                      `json:"node_limit"`
	AsOf      time.Time                `json:"as_of"`
}

type trafficNodeSeriesTotal struct {
	NodeID    uint  `gorm:"column:node_id"`
	UsedBytes int64 `gorm:"column:used_bytes"`
}

func validateTrafficNodeSeriesWindow(bucket trafficUsageBucketSpec, window historyWindow, nodeFiltered bool) error {
	maxDays := historyMaxWindowDays
	switch bucket.Name {
	case trafficUsageBucketMinute:
		maxDays = trafficNodeSeriesMinuteMaxDays
		if nodeFiltered {
			maxDays = trafficNodeSeriesFilteredMinuteMaxDays
		}
	case trafficUsageBucketHour:
		maxDays = trafficNodeSeriesHourMaxDays
		if nodeFiltered {
			maxDays = trafficNodeSeriesFilteredHourMaxDays
		}
	}
	if window.To.Sub(window.From) > time.Duration(maxDays)*24*time.Hour {
		return fmt.Errorf("%s node series supports at most %d days", bucket.Name, maxDays)
	}
	return nil
}

// trafficNodeSeriesHandler returns a read-only chart projection over TrafficRecord.
// It intentionally groups away Subscription/multiplier after applying the
// optional subscription filter: the chart answers how much charged traffic each
// node consumed over time, while the paged details retain the billing dimensions.
func (h *handlers) trafficNodeSeriesHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
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
	query := applyHistoryWindow(h.db.WithContext(r.Context()).Model(&model.TrafficRecord{}), "record_at", window)
	if adminScope {
		if userID, parseErr := positiveQueryID(r.URL.Query(), "user_id"); parseErr != nil {
			BadRequest(w, parseErr.Error())
			return
		} else if userID > 0 {
			query = query.Where("user_id = ?", userID)
		}
	} else {
		query = query.Where("user_id = ?", claims.UserID)
	}
	if subscriptionID, parseErr := positiveQueryID(r.URL.Query(), "subscription_id"); parseErr != nil {
		BadRequest(w, parseErr.Error())
		return
	} else if subscriptionID > 0 {
		query = query.Where("subscription_id = ?", subscriptionID)
	}
	nodeID, parseErr := positiveQueryID(r.URL.Query(), "node_id")
	if parseErr != nil {
		BadRequest(w, parseErr.Error())
		return
	}
	if err := validateTrafficNodeSeriesWindow(bucket, window, nodeID > 0); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if nodeID > 0 {
		query = query.Where("node_id = ?", nodeID)
	}

	truncated := false
	if nodeID == 0 {
		totals := make([]trafficNodeSeriesTotal, 0, trafficNodeSeriesNodeLimit+1)
		if err := query.Session(&gorm.Session{}).
			Select("node_id, COALESCE(SUM(used_bytes), 0) AS used_bytes").
			Where("node_id > 0").
			Group("node_id").
			Order("COALESCE(SUM(used_bytes), 0) DESC, node_id ASC").
			Limit(trafficNodeSeriesNodeLimit + 1).
			Scan(&totals).Error; err != nil {
			ServerError(w, err)
			return
		}
		if len(totals) > trafficNodeSeriesNodeLimit {
			truncated = true
			totals = totals[:trafficNodeSeriesNodeLimit]
		}
		selectedNodeIDs := make([]uint, 0, len(totals))
		for _, total := range totals {
			selectedNodeIDs = append(selectedNodeIDs, total.NodeID)
		}
		if len(selectedNodeIDs) == 0 {
			OK(w, trafficNodeSeriesResponse{
				Bucket: bucket.Name, From: window.From, To: window.To,
				Points: []trafficNodeSeriesPoint{}, Nodes: []entityReference{},
				Truncated: false, NodeLimit: trafficNodeSeriesNodeLimit, AsOf: time.Now().UTC(),
			})
			return
		}
		query = query.Where("node_id IN ?", selectedNodeIDs)
	}

	points := make([]trafficNodeSeriesPoint, 0)
	if err := query.Session(&gorm.Session{}).
		Select(`
			` + bucket.Expression + ` AS record_at,
			node_id,
			COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
			COALESCE(SUM(upload_bytes), 0) AS upload_bytes,
			COALESCE(SUM(download_bytes), 0) AS download_bytes,
			COALESCE(SUM(used_bytes), 0) AS used_bytes,
			COUNT(*) AS record_count
		`).
		Group(bucket.Expression + ", node_id").
		Order("record_at asc, node_id asc").
		Scan(&points).Error; err != nil {
		ServerError(w, err)
		return
	}

	nodeIDs := make(map[uint]struct{})
	for _, point := range points {
		if point.NodeID > 0 {
			nodeIDs[point.NodeID] = struct{}{}
		}
	}
	nodeMap := prefillEntityReferences("node", nodeIDs)
	if err := h.resolveNodeReferences(nodeMap, sortedEntityIDs(nodeIDs)); err != nil {
		ServerError(w, err)
		return
	}
	nodes := make([]entityReference, 0, len(nodeIDs))
	for _, id := range sortedEntityIDs(nodeIDs) {
		nodes = append(nodes, nodeMap[entityKey(id)])
	}

	OK(w, trafficNodeSeriesResponse{
		Bucket:    bucket.Name,
		From:      window.From,
		To:        window.To,
		Points:    points,
		Nodes:     nodes,
		Truncated: truncated,
		NodeLimit: trafficNodeSeriesNodeLimit,
		AsOf:      time.Now().UTC(),
	})
}
