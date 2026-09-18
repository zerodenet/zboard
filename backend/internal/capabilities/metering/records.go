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
type RecordsRepository interface {
	Read(context.Context, uint, RecordsQuery) (RecordsSnapshot, error)
}
type Records struct{ Repository RecordsRepository }

func (s Records) Read(ctx context.Context, actor uint, q RecordsQuery) (RecordsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return RecordsSnapshot{}, err
	}
	if actor == 0 {
		return RecordsSnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
	}
	invalid := func(message string) (RecordsSnapshot, error) {
		return RecordsSnapshot{}, &PolicyValidation{Fields: map[string]string{"query": message}}
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
	return s.Repository.Read(ctx, actor, q)
}
