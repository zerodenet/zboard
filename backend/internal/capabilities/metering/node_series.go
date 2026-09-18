package metering

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const NodeSeriesNodeLimit = 8

type NodeSeriesQuery struct {
	Administrative                 bool
	UserID, SubscriptionID, NodeID uint
	Bucket                         string
	From, To                       time.Time
}
type NodeSeriesPoint struct {
	RecordAt      time.Time `json:"record_at"`
	NodeID        uint      `json:"node_id"`
	RawBytes      int64     `json:"raw_bytes"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
	UsedBytes     int64     `json:"used_bytes"`
	RecordCount   int64     `json:"record_count"`
}
type SeriesNode struct {
	ID                   uint
	Name, Region, Status string
	Missing              bool
}
type NodeSeriesSnapshot struct {
	Points    []NodeSeriesPoint
	Nodes     []SeriesNode
	Truncated bool
	AsOf      time.Time
}
type NodeSeriesRepository interface {
	Read(context.Context, uint, NodeSeriesQuery) (NodeSeriesSnapshot, error)
}
type NodeSeries struct{ Repository NodeSeriesRepository }

func ValidateNodeSeriesWindow(bucket string, from, to time.Time, nodeFiltered bool) error {
	maxDays := 366
	switch bucket {
	case "minute":
		maxDays = 1
		if nodeFiltered {
			maxDays = 7
		}
	case "hour":
		maxDays = 31
		if nodeFiltered {
			maxDays = 366
		}
	case "day":
	default:
		return fmt.Errorf("bucket must be minute, hour or day")
	}
	if from.IsZero() || !to.After(from) {
		return fmt.Errorf("invalid node series window")
	}
	if to.Sub(from) > time.Duration(maxDays)*24*time.Hour {
		return fmt.Errorf("%s node series supports at most %d days", bucket, maxDays)
	}
	return nil
}
func (s NodeSeries) Read(ctx context.Context, actor uint, q NodeSeriesQuery) (NodeSeriesSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return NodeSeriesSnapshot{}, err
	}
	if actor == 0 {
		return NodeSeriesSnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	q.Bucket = strings.ToLower(strings.TrimSpace(q.Bucket))
	if q.Bucket == "" {
		q.Bucket = "minute"
	}
	if err := ValidateNodeSeriesWindow(q.Bucket, q.From, q.To, q.NodeID > 0); err != nil {
		return NodeSeriesSnapshot{}, &PolicyValidation{Fields: map[string]string{"range": err.Error()}}
	}
	return s.Repository.Read(ctx, actor, q)
}
