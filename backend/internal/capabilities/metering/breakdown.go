package metering

import (
	"context"
	"time"
)

type BreakdownQuery struct {
	Administrative                                     bool
	UserID, SubscriptionID, NodeID, ProtocolEndpointID uint
	Dimension                                          string
	From, To                                           time.Time
	Offset, Limit                                      int
}

type BreakdownCounters struct {
	RawBytes      int64 `json:"raw_bytes"`
	UploadBytes   int64 `json:"upload_bytes"`
	DownloadBytes int64 `json:"download_bytes"`
	UsedBytes     int64 `json:"used_bytes"`
	RecordCount   int64 `json:"record_count"`
}

type BreakdownRow struct {
	ID                uint   `json:"id"`
	Name              string `json:"name"`
	Secondary         string `json:"secondary,omitempty"`
	Missing           bool   `json:"missing"`
	BreakdownCounters `gorm:"embedded"`
}

type BreakdownSnapshot struct {
	Dimension string            `json:"dimension"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
	AsOf      time.Time         `json:"as_of"`
	Items     []BreakdownRow    `json:"items"`
	Total     int64             `json:"total"`
	Totals    BreakdownCounters `json:"totals"`
	Offset    int               `json:"offset"`
	Limit     int               `json:"limit"`
}

type BreakdownRepository interface {
	Read(context.Context, uint, BreakdownQuery) (BreakdownSnapshot, error)
}

type BreakdownCache interface {
	Get(context.Context, [32]byte, func() (BreakdownSnapshot, error)) (BreakdownSnapshot, error)
}

type Breakdown struct{ Repository BreakdownRepository }

func (s Breakdown) Read(ctx context.Context, actor uint, q BreakdownQuery) (BreakdownSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return BreakdownSnapshot{}, err
	}
	if actor == 0 {
		return BreakdownSnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	invalid := func(message string) (BreakdownSnapshot, error) {
		return BreakdownSnapshot{}, &PolicyValidation{Fields: map[string]string{"query": message}}
	}
	if q.Dimension != "user" && q.Dimension != "node" && q.Dimension != "subscription" && q.Dimension != "protocol_endpoint" {
		return invalid("invalid statistics dimension")
	}
	if q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
		return invalid("invalid history window")
	}
	if q.Offset < 0 || q.Limit < 1 || q.Limit > 200 {
		return invalid("invalid pagination")
	}
	return s.Repository.Read(ctx, actor, q)
}
