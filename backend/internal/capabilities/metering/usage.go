package metering

import (
	"context"
	"strings"
	"time"
)

type UsageRow struct {
	ID                      uint      `json:"id"`
	UserID                  uint      `json:"user_id"`
	SubscriptionID          uint      `json:"subscription_id,omitempty"`
	NodeID                  uint      `json:"node_id"`
	RawBytes                int64     `json:"raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes"`
	RecordAt                time.Time `json:"record_at"`
	RecordCount             int64     `json:"record_count"`
}

type UsageReference struct {
	ID                            uint
	Kind, Name, Secondary, Status string
	Missing                       bool
}
type UsageQuery struct {
	RecordsQuery
	Bucket                     string
	IncludeTotals, SummaryOnly bool
}
type UsageSnapshot struct {
	Rows       []UsageRow
	Statistics *UsageStatistics
	HasMore    bool
	References []UsageReference
}
type UsageRepository interface {
	Read(context.Context, uint, UsageQuery) (UsageSnapshot, error)
}
type Usage struct{ Repository UsageRepository }

func (s Usage) Read(ctx context.Context, actor uint, q UsageQuery) (UsageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return UsageSnapshot{}, err
	}
	if actor == 0 {
		return UsageSnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	q.Bucket = strings.ToLower(strings.TrimSpace(q.Bucket))
	if q.Bucket == "" {
		q.Bucket = "minute"
	}
	invalid := func(message string) (UsageSnapshot, error) {
		return UsageSnapshot{}, &PolicyValidation{Fields: map[string]string{"query": message}}
	}
	if q.Bucket != "minute" && q.Bucket != "hour" && q.Bucket != "day" {
		return invalid("invalid bucket")
	}
	if q.Offset < 0 || q.Limit < 1 || q.Limit > 200 {
		return invalid("invalid pagination")
	}
	if q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
		return invalid("invalid history window")
	}
	if q.Cursor != nil && (q.Cursor.ID == 0 || q.Cursor.At.IsZero() || (q.Cursor.Direction != "older" && q.Cursor.Direction != "newer")) {
		return invalid("invalid cursor")
	}
	return s.Repository.Read(ctx, actor, q)
}
