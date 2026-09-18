package metering

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type ObservationRange struct {
	Name           string
	Duration       time.Duration
	BucketDuration time.Duration
}

type ObservationBucket struct {
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	ConnectionStarts int64     `json:"connection_starts"`
	WorkingNodes     int64     `json:"working_nodes"`
}

type ObservationSeries struct {
	SubscriptionID        uint                `json:"subscription_id"`
	Range                 string              `json:"range"`
	Since                 time.Time           `json:"since"`
	Until                 time.Time           `json:"until"`
	BucketSeconds         int                 `json:"bucket_seconds"`
	RetentionDays         int                 `json:"retention_days"`
	TimeBasis             string              `json:"time_basis"`
	TelemetryCompleteness string              `json:"telemetry_completeness"`
	Coverage              CoverageSummary     `json:"coverage"`
	TotalConnectionStarts int64               `json:"total_connection_starts"`
	DistinctWorkingNodes  int64               `json:"distinct_working_nodes"`
	ActiveBuckets         int                 `json:"active_buckets"`
	MaxConnectionStarts   int64               `json:"max_connection_starts_per_bucket"`
	P50ConnectionStarts   int64               `json:"p50_connection_starts_per_bucket"`
	P95ConnectionStarts   int64               `json:"p95_connection_starts_per_bucket"`
	MaxWorkingNodes       int64               `json:"max_working_nodes_per_bucket"`
	P50WorkingNodes       int64               `json:"p50_working_nodes_per_bucket"`
	P95WorkingNodes       int64               `json:"p95_working_nodes_per_bucket"`
	Buckets               []ObservationBucket `json:"buckets"`
}

var observationRanges = map[string]ObservationRange{
	"1d":  {Name: "1d", Duration: 24 * time.Hour, BucketDuration: 5 * time.Minute},
	"3d":  {Name: "3d", Duration: 3 * 24 * time.Hour, BucketDuration: 15 * time.Minute},
	"7d":  {Name: "7d", Duration: 7 * 24 * time.Hour, BucketDuration: time.Hour},
	"15d": {Name: "15d", Duration: 15 * 24 * time.Hour, BucketDuration: time.Hour},
}

func ParseObservationRange(raw string) (ObservationRange, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = "1d"
	}
	spec, ok := observationRanges[value]
	if !ok {
		return ObservationRange{}, errors.New("range must be one of 1d, 3d, 7d, 15d")
	}
	return spec, nil
}

type ObservationAggregate struct {
	BucketIndex      int64
	ConnectionStarts int64
	WorkingNodes     int64
}

func ObservationPercentile(values []int64, percentile float64) int64 {
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

type ObservationSeriesRepository interface {
	Read(context.Context, uint, uint, ObservationRange, time.Time) (ObservationSeries, error)
}
type ObservationSeriesService struct{ Repository ObservationSeriesRepository }

func (s ObservationSeriesService) Read(ctx context.Context, actor, subscription uint, window string, now time.Time) (ObservationSeries, error) {
	if err := validateScope(actor, PolicyScope{Type: "subscription", ID: subscription}); err != nil {
		return ObservationSeries{}, err
	}
	spec, err := ParseObservationRange(window)
	if err != nil {
		return ObservationSeries{}, &PolicyValidation{Fields: map[string]string{"range": err.Error()}}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Repository.Read(ctx, actor, subscription, spec, now.UTC())
}
func ProjectObservationSeries(subscriptionID uint, spec ObservationRange, now time.Time, coverage CoverageSummary, rows []ObservationAggregate, distinctWorkingNodes int64) (ObservationSeries, error) {
	since := now.Add(-spec.Duration)
	bucketSeconds := int64(spec.BucketDuration / time.Second)
	if bucketSeconds <= 0 {
		return ObservationSeries{}, errors.New("invalid observation bucket")
	}
	byBucket := make(map[int64]ObservationAggregate, len(rows))
	for _, row := range rows {
		byBucket[row.BucketIndex] = row
	}
	firstBucket := since.Unix() / bucketSeconds
	lastBucket := now.Unix() / bucketSeconds
	buckets := make([]ObservationBucket, 0, int(lastBucket-firstBucket+1))
	connectionValues := make([]int64, 0, int(lastBucket-firstBucket+1))
	workingNodeValues := make([]int64, 0, int(lastBucket-firstBucket+1))

	series := ObservationSeries{
		SubscriptionID:        subscriptionID,
		Range:                 spec.Name,
		Since:                 since,
		Until:                 now,
		BucketSeconds:         int(bucketSeconds),
		RetentionDays:         15,
		TimeBasis:             "zboard_receive_time",
		TelemetryCompleteness: coverage.State,
		Coverage:              coverage,
		DistinctWorkingNodes:  distinctWorkingNodes,
	}
	for bucketIndex := firstBucket; bucketIndex <= lastBucket; bucketIndex++ {
		row := byBucket[bucketIndex]
		start := time.Unix(bucketIndex*bucketSeconds, 0).UTC()
		end := start.Add(spec.BucketDuration)
		bucket := ObservationBucket{
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
	series.P50ConnectionStarts = ObservationPercentile(connectionValues, 0.50)
	series.P95ConnectionStarts = ObservationPercentile(connectionValues, 0.95)
	series.P50WorkingNodes = ObservationPercentile(workingNodeValues, 0.50)
	series.P95WorkingNodes = ObservationPercentile(workingNodeValues, 0.95)
	series.Buckets = buckets
	return series, nil
}
