package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Transitional business adapter; the admitted task runner owns lifecycle.
func (h *handlers) ExecuteBatchItem(ctx context.Context, batch jobs.BatchReceipt, target jobs.BatchItem, token string) ([]string, bool) {
	failures := h.services.BatchItemRunner(h).Run(ctx, jobs.BatchItemClaim{TaskID: batch.ID, ItemID: target.ID, Token: token})
	return failures, batch.Type == taskTypeNodeGroupSync && target.TargetType == "node_group" && len(failures) > 0
}
func (h *handlers) ExecuteBatchBusiness(ctx context.Context, batch jobs.BatchReceipt, target jobs.BatchItem) error {
	task := model.Task{ID: batch.ID, Type: batch.Type, Scope: batch.Scope, Content: batch.Content, Status: batch.Status, Errors: batch.Errors, Total: batch.Total, Current: batch.Current, IdempotencyKey: batch.IdempotencyKey, Priority: batch.Priority, ScheduledAt: batch.ScheduledAt, StartedAt: batch.StartedAt, FinishedAt: batch.FinishedAt, Attempts: batch.Attempts, MaxAttempts: batch.MaxAttempts, LockedBy: batch.LockedBy, LockedUntil: batch.LockedUntil, CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt}
	item := model.TaskItem{DeliveryState: target.DeliveryState, ID: target.ID, TaskID: target.TaskID, TargetType: target.TargetType, TargetID: target.TargetID, Payload: target.Payload, Status: target.Status, Attempts: target.Attempts, Error: target.Error, StartedAt: target.StartedAt, FinishedAt: target.FinishedAt, CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt}
	return h.executeTaskItemContext(ctx, task, item)
}
