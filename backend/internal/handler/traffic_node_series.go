package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

type trafficNodeSeriesPoint struct {
	RecordAt      trafficBucketTime `json:"record_at" gorm:"column:record_at"`
	NodeID        uint              `json:"node_id" gorm:"column:node_id"`
	RawBytes      int64             `json:"raw_bytes" gorm:"column:raw_bytes"`
	UploadBytes   int64             `json:"upload_bytes" gorm:"column:upload_bytes"`
	DownloadBytes int64             `json:"download_bytes" gorm:"column:download_bytes"`
	UsedBytes     int64             `json:"used_bytes" gorm:"column:used_bytes"`
	RecordCount   int64             `json:"record_count" gorm:"column:record_count"`
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

func validateTrafficNodeSeriesWindow(bucket trafficUsageBucketSpec, window historyWindow, nodeFiltered bool) error {
	return metering.ValidateNodeSeriesWindow(bucket.Name, window.From, window.To, nodeFiltered)
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

	userID := uint(0)
	if adminScope {
		userID, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	subscriptionID, err := positiveQueryID(r.URL.Query(), "subscription_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	nodeID, err := positiveQueryID(r.URL.Query(), "node_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.NodeSeries().Read(r.Context(), claims.UserID, metering.NodeSeriesQuery{Administrative: adminScope, UserID: userID, SubscriptionID: subscriptionID, NodeID: nodeID, Bucket: bucket.Name, From: window.From, To: window.To})
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	points := make([]trafficNodeSeriesPoint, 0, len(result.Points))
	for _, p := range result.Points {
		points = append(points, trafficNodeSeriesPoint{RecordAt: trafficBucketTime{Time: p.RecordAt}, NodeID: p.NodeID, RawBytes: p.RawBytes, UploadBytes: p.UploadBytes, DownloadBytes: p.DownloadBytes, UsedBytes: p.UsedBytes, RecordCount: p.RecordCount})
	}
	nodes := make([]entityReference, 0, len(result.Nodes))
	for _, n := range result.Nodes {
		if n.Missing {
			nodes = append(nodes, missingEntityReference("node", n.ID))
			continue
		}
		name := n.Name
		if name == "" {
			name = "节点"
		}
		nodes = append(nodes, entityReference{ID: n.ID, Kind: "node", DisplayName: name, Secondary: n.Region, Status: n.Status})
	}
	OK(w, trafficNodeSeriesResponse{Bucket: bucket.Name, From: window.From, To: window.To, Points: points, Nodes: nodes, Truncated: result.Truncated, NodeLimit: metering.NodeSeriesNodeLimit, AsOf: result.AsOf})
}
