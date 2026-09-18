package metering

import (
	"context"
)

type ReconciliationItem struct {
	SubscriptionID uint   `json:"subscription_id"`
	UserID         uint   `json:"user_id"`
	PlanID         uint   `json:"plan_id"`
	Status         string `json:"status"`
	FlowUsed       int64  `json:"flow_used"`
	RecordedBytes  int64  `json:"recorded_bytes"`
	Difference     int64  `json:"difference"`
	Result         string `json:"result"`
}

type ReconciliationAggregates struct {
	SubscriptionCount   int64 `json:"subscription_count"`
	MatchedCount        int64 `json:"matched_count"`
	MissingRecordsCount int64 `json:"missing_records_count"`
	OverRecordedCount   int64 `json:"over_recorded_count"`
	FlowUsed            int64 `json:"flow_used"`
	RecordedBytes       int64 `json:"recorded_bytes"`
	MissingBytes        int64 `json:"missing_bytes"`
	OverRecordedBytes   int64 `json:"over_recorded_bytes"`
}

type ReconciliationQuery struct {
	Administrative         bool
	UserID, SubscriptionID uint
	Paged, IssuesOnly      bool
	Offset, Limit          int
}
type ReconciliationSnapshot struct {
	Items      []ReconciliationItem
	Total      int64
	Aggregates ReconciliationAggregates
}
type ReconciliationRepository interface {
	Read(context.Context, uint, ReconciliationQuery) (ReconciliationSnapshot, error)
}
type Reconciliation struct{ Repository ReconciliationRepository }

func (s Reconciliation) Read(ctx context.Context, actor uint, q ReconciliationQuery) (ReconciliationSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ReconciliationSnapshot{}, err
	}
	if actor == 0 {
		return ReconciliationSnapshot{}, ErrTrendPermission
	}
	if !q.Administrative {
		q.UserID = actor
		q.Paged = false
		q.IssuesOnly = false
	}
	if q.Paged && (q.Offset < 0 || q.Limit < 1 || q.Limit > 200) {
		return ReconciliationSnapshot{}, &PolicyValidation{Fields: map[string]string{"page": "invalid pagination"}}
	}
	return s.Repository.Read(ctx, actor, q)
}
func ReconciliationResult(difference int64) string {
	if difference > 0 {
		return "missing_records"
	}
	if difference < 0 {
		return "over_recorded"
	}
	return "matched"
}
