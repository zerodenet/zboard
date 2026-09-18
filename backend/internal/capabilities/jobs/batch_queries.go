package jobs

import (
	"context"
	"errors"
	"time"
)

var ErrBatchPermission = errors.New("current administrator required")
var ErrBatchNotFound = errors.New("task not found")
var ErrBatchQuery = errors.New("invalid task query")

type BatchItem struct {
	DeliveryState string     `json:"delivery_state,omitempty"`
	ID            uint       `json:"id"`
	TaskID        uint       `json:"task_id"`
	TargetType    string     `json:"target_type"`
	TargetID      string     `json:"target_id"`
	Payload       string     `json:"payload"`
	Status        int16      `json:"status"`
	Attempts      int        `json:"attempts"`
	Error         string     `json:"error"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
type BatchView struct {
	BatchReceipt
	PendingCount   int64 `json:"pending_count"`
	RunningCount   int64 `json:"running_count"`
	SucceededCount int64 `json:"succeeded_count"`
	FailedCount    int64 `json:"failed_count"`
}
type BatchDetail struct {
	BatchView
	Items []BatchItem `json:"items,omitempty"`
}
type BatchSummary struct {
	Total            int64 `json:"total"`
	Pending          int64 `json:"pending"`
	Running          int64 `json:"running"`
	Completed        int64 `json:"completed"`
	Failed           int64 `json:"failed"`
	ActiveCurrent    int64 `json:"active_current"`
	ActiveTotal      int64 `json:"active_total"`
	PendingTargets   int64 `json:"pending_targets"`
	RunningTargets   int64 `json:"running_targets"`
	SucceededTargets int64 `json:"succeeded_targets"`
	FailedTargets    int64 `json:"failed_targets"`
}
type BatchFilter struct {
	Type          string
	Status        *int16
	Limit, Offset int
}
type BatchPage struct {
	Items  []BatchView `json:"items"`
	Total  int64       `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
}
type BatchItemPage struct {
	Items  []BatchItem `json:"items"`
	Total  int64       `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
}
type BatchQueryRepository interface {
	List(context.Context, uint, BatchFilter) (BatchPage, error)
	Detail(context.Context, uint, uint, bool) (BatchDetail, error)
	Items(context.Context, uint, uint, BatchFilter) (BatchItemPage, error)
	Summary(context.Context, uint) (BatchSummary, error)
}
type BatchQueries struct{ Repository BatchQueryRepository }

func validBatchFilter(f BatchFilter) bool {
	return f.Limit >= 1 && f.Limit <= 100 && f.Offset >= 0 && f.Offset <= 1000000 && len(f.Type) <= 32 && (f.Status == nil || (*f.Status >= 0 && *f.Status <= 3))
}
func (s BatchQueries) List(ctx context.Context, actor uint, f BatchFilter) (BatchPage, error) {
	if actor == 0 {
		return BatchPage{}, ErrBatchPermission
	}
	if !validBatchFilter(f) {
		return BatchPage{}, ErrBatchQuery
	}
	return s.Repository.List(ctx, actor, f)
}
func (s BatchQueries) Detail(ctx context.Context, actor, id uint, items bool) (BatchDetail, error) {
	if actor == 0 {
		return BatchDetail{}, ErrBatchPermission
	}
	if id == 0 {
		return BatchDetail{}, ErrBatchQuery
	}
	return s.Repository.Detail(ctx, actor, id, items)
}
func (s BatchQueries) Items(ctx context.Context, actor, id uint, f BatchFilter) (BatchItemPage, error) {
	if actor == 0 {
		return BatchItemPage{}, ErrBatchPermission
	}
	if id == 0 || !validBatchFilter(f) {
		return BatchItemPage{}, ErrBatchQuery
	}
	return s.Repository.Items(ctx, actor, id, f)
}
func (s BatchQueries) Summary(ctx context.Context, actor uint) (BatchSummary, error) {
	if actor == 0 {
		return BatchSummary{}, ErrBatchPermission
	}
	return s.Repository.Summary(ctx, actor)
}
