package metering

import (
	"context"
	"time"
)

type RecordAggregates struct {
	RawBytes              int64 `json:"raw_bytes"`
	UsedBytes             int64 `json:"used_bytes"`
	UserCount             int64 `json:"user_count"`
	SubscriptionCount     int64 `json:"subscription_count"`
	NodeCount             int64 `json:"node_count"`
	ProtocolEndpointCount int64 `json:"protocol_endpoint_count"`
}

type RecordCursor struct {
	At        time.Time
	ID        uint
	Direction string
}
type RecordsQuery struct {
	Administrative                                     bool
	UserID, SubscriptionID, NodeID, ProtocolEndpointID uint
	Paged                                              bool
	Offset, Limit                                      int
	From, To                                           time.Time
	Cursor                                             *RecordCursor
}
type RecordsSnapshot struct {
	Records    []TrafficRecord
	Total      int64
	Aggregates RecordAggregates
	HasMore    bool
}

// TrafficRecordSummary is the explicit public raw-page projection. Full reads
// keep TrafficRecord, including its accounting evidence and provenance fields.
type TrafficRecordSummary struct {
	ID                      uint      `json:"id"`
	UserID                  uint      `json:"user_id"`
	SubscriptionID          uint      `json:"subscription_id,omitempty"`
	NodeID                  uint      `json:"node_id"`
	ProtocolEndpointID      uint      `json:"protocol_endpoint_id"`
	RawBytes                int64     `json:"raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes"`
	At                      time.Time `json:"record_at"`
}

type RecordsSummaryPage struct {
	Records    []TrafficRecordSummary
	Total      int64
	Aggregates RecordAggregates
	HasMore    bool
}

type RecordsRepository interface {
	Read(context.Context, uint, RecordsQuery) (RecordsSnapshot, error)
	ReadSummaryPage(context.Context, uint, RecordsQuery) (RecordsSummaryPage, error)
}
type Records struct{ Repository RecordsRepository }

func (s Records) Read(ctx context.Context, actor uint, q RecordsQuery) (RecordsSnapshot, error) {
	q, err := validateRecordsRead(ctx, actor, q)
	if err != nil {
		return RecordsSnapshot{}, err
	}
	return s.Repository.Read(ctx, actor, q)
}

// ReadSummaryPage must be selected explicitly. Paged alone never changes the
// full Read contract, and raw unpaged callers keep all recorded evidence.
func (s Records) ReadSummaryPage(ctx context.Context, actor uint, q RecordsQuery) (RecordsSummaryPage, error) {
	q, err := validateRecordsRead(ctx, actor, q)
	if err != nil {
		return RecordsSummaryPage{}, err
	}
	if !q.Paged {
		return RecordsSummaryPage{}, &PolicyValidation{Fields: map[string]string{"query": "summary requires pagination"}}
	}
	return s.Repository.ReadSummaryPage(ctx, actor, q)
}

func validateRecordsRead(ctx context.Context, actor uint, q RecordsQuery) (RecordsQuery, error) {
	if err := ctx.Err(); err != nil {
		return q, err
	}
	if actor == 0 {
		return q, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	invalid := func(message string) (RecordsQuery, error) {
		return q, &PolicyValidation{Fields: map[string]string{"query": message}}
	}
	if q.Paged {
		if q.Offset < 0 || q.Limit < 1 || q.Limit > 200 {
			return invalid("invalid pagination")
		}
		if q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
			return invalid("invalid history window")
		}
		if q.Cursor != nil && (q.Cursor.At.IsZero() || q.Cursor.ID == 0 || (q.Cursor.Direction != "older" && q.Cursor.Direction != "newer")) {
			return invalid("invalid cursor")
		}
	}
	return q, nil
}
