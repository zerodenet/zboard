package metering

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type TrafficTrendAggregate struct {
	Day           string
	UploadBytes   int64
	DownloadBytes int64
	UsedBytes     int64
	RecordCount   int64
}

type TrafficTrendPoint struct {
	Date            string `json:"date"`
	Label           string `json:"label"`
	UploadBytes     int64  `json:"upload_bytes"`
	DownloadBytes   int64  `json:"download_bytes"`
	UsedBytes       int64  `json:"used_bytes"`
	PeakConnections *int64 `json:"peak_connections"`
	RecordCount     int64  `json:"record_count"`
}

func BuildTrafficTrendPoints(from time.Time, days int, rows []TrafficTrendAggregate) ([]TrafficTrendPoint, int64) {
	byDay := make(map[string]TrafficTrendAggregate, len(rows))
	var recordCount int64
	for _, row := range rows {
		key := strings.TrimSpace(row.Day)
		if len(key) >= 10 {
			key = key[:10]
		}
		byDay[key] = row
		recordCount += row.RecordCount
	}
	points := make([]TrafficTrendPoint, 0, days)
	for index := 0; index < days; index++ {
		date := from.AddDate(0, 0, index)
		key := date.Format("2006-01-02")
		row := byDay[key]
		points = append(points, TrafficTrendPoint{
			Date:            key,
			Label:           fmt.Sprintf("%d/%d", int(date.Month()), date.Day()),
			UploadBytes:     row.UploadBytes,
			DownloadBytes:   row.DownloadBytes,
			UsedBytes:       row.UsedBytes,
			PeakConnections: nil,
			RecordCount:     row.RecordCount,
		})
	}
	return points, recordCount
}

type TrafficTrendSnapshot struct {
	Points      []TrafficTrendPoint
	RecordCount int64
	AsOf        time.Time
}
type TrafficTrendReference struct {
	ID                             uint
	DisplayName, Secondary, Status string
}
type TrafficTrendData struct {
	Snapshot      TrafficTrendSnapshot
	Subscriptions []TrafficTrendReference
}
type TrafficTrendQuery struct {
	Administrative                                     bool
	UserID, SubscriptionID, NodeID, ProtocolEndpointID uint
	IncludeSubscriptions                               bool
	From                                               time.Time
	Days                                               int
	Timezone                                           string
	Buckets                                            []TrendBucket
}
type TrafficTrendCache interface {
	Get(context.Context, [32]byte, func() (TrafficTrendSnapshot, error)) (TrafficTrendSnapshot, error)
}
type TrafficTrendRepository interface {
	Read(context.Context, uint, TrafficTrendQuery) (TrafficTrendData, error)
}
type TrafficTrends struct{ Repository TrafficTrendRepository }

func (s TrafficTrends) Read(ctx context.Context, actor uint, q TrafficTrendQuery) (TrafficTrendData, error) {
	if actor == 0 {
		return TrafficTrendData{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	if q.Days != len(q.Buckets) || q.Days < 1 || q.Days > 366 || q.From.IsZero() {
		return TrafficTrendData{}, &PolicyValidation{Fields: map[string]string{"range": "invalid trend range"}}
	}
	for i, b := range q.Buckets {
		day := q.From.AddDate(0, 0, i)
		if b.Key != day.Format("2006-01-02") || !b.StartUTC.Equal(day.UTC()) || !b.EndUTC.Equal(day.AddDate(0, 0, 1).UTC()) {
			return TrafficTrendData{}, &PolicyValidation{Fields: map[string]string{"range": "invalid calendar buckets"}}
		}
	}
	return s.Repository.Read(ctx, actor, q)
}
