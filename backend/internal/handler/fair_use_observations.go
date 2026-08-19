package handler

import (
	"errors"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type fairUseObservationRange struct {
	Name           string
	Duration       time.Duration
	BucketDuration time.Duration
}

type fairUseObservationBucket struct {
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	ConnectionStarts int64     `json:"connection_starts"`
	WorkingNodes     int64     `json:"working_nodes"`
}

type fairUseObservationSeries struct {
	SubscriptionID        uint                       `json:"subscription_id"`
	Range                 string                     `json:"range"`
	Since                 time.Time                  `json:"since"`
	Until                 time.Time                  `json:"until"`
	BucketSeconds         int                        `json:"bucket_seconds"`
	RetentionDays         int                        `json:"retention_days"`
	TimeBasis             string                     `json:"time_basis"`
	TelemetryCompleteness string                     `json:"telemetry_completeness"`
	Coverage              fairUseCoverageSummary     `json:"coverage"`
	TotalConnectionStarts int64                      `json:"total_connection_starts"`
	DistinctWorkingNodes  int64                      `json:"distinct_working_nodes"`
	ActiveBuckets         int                        `json:"active_buckets"`
	MaxConnectionStarts   int64                      `json:"max_connection_starts_per_bucket"`
	P50ConnectionStarts   int64                      `json:"p50_connection_starts_per_bucket"`
	P95ConnectionStarts   int64                      `json:"p95_connection_starts_per_bucket"`
	MaxWorkingNodes       int64                      `json:"max_working_nodes_per_bucket"`
	P50WorkingNodes       int64                      `json:"p50_working_nodes_per_bucket"`
	P95WorkingNodes       int64                      `json:"p95_working_nodes_per_bucket"`
	Buckets               []fairUseObservationBucket `json:"buckets"`
}

var fairUseObservationRanges = map[string]fairUseObservationRange{
	"1d":  {Name: "1d", Duration: 24 * time.Hour, BucketDuration: 5 * time.Minute},
	"3d":  {Name: "3d", Duration: 3 * 24 * time.Hour, BucketDuration: 15 * time.Minute},
	"7d":  {Name: "7d", Duration: 7 * 24 * time.Hour, BucketDuration: time.Hour},
	"15d": {Name: "15d", Duration: 15 * 24 * time.Hour, BucketDuration: time.Hour},
}

func parseFairUseObservationRange(raw string) (fairUseObservationRange, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = "1d"
	}
	spec, ok := fairUseObservationRanges[value]
	if !ok {
		return fairUseObservationRange{}, errors.New("range must be one of 1d, 3d, 7d, 15d")
	}
	return spec, nil
}

type fairUseObservationAggregateRow struct {
	BucketIndex      int64 `gorm:"column:bucket_index"`
	ConnectionStarts int64 `gorm:"column:connection_starts"`
	WorkingNodes     int64 `gorm:"column:working_nodes"`
}

func fairUsePercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func (h *handlers) loadFairUseObservationSeries(subscriptionID uint, spec fairUseObservationRange, now time.Time) (fairUseObservationSeries, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	since := now.Add(-spec.Duration)
	bucketSeconds := int64(spec.BucketDuration / time.Second)
	if bucketSeconds <= 0 {
		return fairUseObservationSeries{}, errors.New("invalid observation bucket")
	}

	coverage, err := h.fairUseCoverageForSubscription(subscriptionID, int(spec.Duration/time.Second), now)
	if err != nil {
		return fairUseObservationSeries{}, err
	}

	var rows []fairUseObservationAggregateRow
	if err := h.db.Table("subscription_flow_start_events").
		Select("FLOOR(TIMESTAMPDIFF(SECOND, '1970-01-01 00:00:00', received_at) / ?) AS bucket_index, COUNT(*) AS connection_starts, COUNT(DISTINCT node_id) AS working_nodes", bucketSeconds).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscriptionID, since, now).
		Group("bucket_index").
		Order("bucket_index asc").
		Scan(&rows).Error; err != nil {
		return fairUseObservationSeries{}, err
	}

	var distinctWorkingNodes int64
	if err := h.db.Model(&subscriptionFlowStartEvent{}).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscriptionID, since, now).
		Distinct("node_id").Count(&distinctWorkingNodes).Error; err != nil {
		return fairUseObservationSeries{}, err
	}

	byBucket := make(map[int64]fairUseObservationAggregateRow, len(rows))
	for _, row := range rows {
		byBucket[row.BucketIndex] = row
	}
	firstBucket := since.Unix() / bucketSeconds
	lastBucket := now.Unix() / bucketSeconds
	buckets := make([]fairUseObservationBucket, 0, int(lastBucket-firstBucket+1))
	connectionValues := make([]int64, 0, int(lastBucket-firstBucket+1))
	workingNodeValues := make([]int64, 0, int(lastBucket-firstBucket+1))

	series := fairUseObservationSeries{
		SubscriptionID:        subscriptionID,
		Range:                 spec.Name,
		Since:                 since,
		Until:                 now,
		BucketSeconds:         int(bucketSeconds),
		RetentionDays:         int(fairUseObservationRetention / (24 * time.Hour)),
		TimeBasis:             "zboard_receive_time",
		TelemetryCompleteness: coverage.State,
		Coverage:              coverage,
		DistinctWorkingNodes:  distinctWorkingNodes,
	}
	for bucketIndex := firstBucket; bucketIndex <= lastBucket; bucketIndex++ {
		row := byBucket[bucketIndex]
		start := time.Unix(bucketIndex*bucketSeconds, 0).UTC()
		end := start.Add(spec.BucketDuration)
		bucket := fairUseObservationBucket{
			StartAt:          start,
			EndAt:            end,
			ConnectionStarts: row.ConnectionStarts,
			WorkingNodes:     row.WorkingNodes,
		}
		buckets = append(buckets, bucket)
		connectionValues = append(connectionValues, row.ConnectionStarts)
		workingNodeValues = append(workingNodeValues, row.WorkingNodes)
		series.TotalConnectionStarts += row.ConnectionStarts
		if row.ConnectionStarts > 0 {
			series.ActiveBuckets++
		}
		if row.ConnectionStarts > series.MaxConnectionStarts {
			series.MaxConnectionStarts = row.ConnectionStarts
		}
		if row.WorkingNodes > series.MaxWorkingNodes {
			series.MaxWorkingNodes = row.WorkingNodes
		}
	}
	series.P50ConnectionStarts = fairUsePercentile(connectionValues, 0.50)
	series.P95ConnectionStarts = fairUsePercentile(connectionValues, 0.95)
	series.P50WorkingNodes = fairUsePercentile(workingNodeValues, 0.50)
	series.P95WorkingNodes = fairUsePercentile(workingNodeValues, 0.95)
	series.Buckets = buckets
	return series, nil
}

func (h *handlers) AdminSubscriptionFairUseObservationsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	subscriptionID, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/observations")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	exists, err := h.fairUseSubscriptionExists(subscriptionID)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !exists {
		NotFound(w)
		return
	}
	spec, err := parseFairUseObservationRange(r.URL.Query().Get("range"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	series, err := h.loadFairUseObservationSeries(subscriptionID, spec, time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, series)
}
