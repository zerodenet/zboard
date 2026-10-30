package observability

import (
	"context"
	"errors"
	"time"
)

const (
	AuditLogRetentionKey        = "audit_log_retention_days"
	OperationRetentionKey       = "operation_history_retention_days"
	TaskRetentionKey            = "task_history_retention_days"
	PeriodicSuccessRetentionKey = "periodic_success_retention_days"
	PeriodicFailureRetentionKey = "periodic_failure_retention_days"
	ArchiveCursorRetentionKey   = "archived_flow_cursor_retention_days"
	RetentionMaxDays            = 3650
	HistoryBatchSize            = 1000
)

type HistoryTarget string

const (
	AuditLogs           HistoryTarget = "audit_logs"
	Tasks               HistoryTarget = "tasks"
	NodeOperations      HistoryTarget = "node_operations"
	ProtocolDeployments HistoryTarget = "protocol_deployments"
	ArchivedFlowCursors HistoryTarget = "archived_flow_cursors"
	PeriodicSuccess     HistoryTarget = "periodic_success"
	PeriodicFailure     HistoryTarget = "periodic_failure"
)

type HistoryRetentionResult struct {
	AuditLogs, Tasks, NodeOperations, ProtocolDeployments  int64
	PeriodicSucceeded, PeriodicFailed, ArchivedFlowCursors int64
	FullBatches                                            []HistoryTarget
}

type HistoryRetentionRepository interface {
	ReconcileDefaults(context.Context) error
	RetentionDays(context.Context, string, int, int) (int, error)
	Prune(context.Context, HistoryTarget, time.Time, int) (int64, error)
	PrunePeriodicRuns(context.Context, time.Time, bool) (int64, error)
	HasPendingHistory(context.Context, HistoryTarget, time.Time) (bool, error)
}

type HistoryRetention struct{ Repository HistoryRetentionRepository }

func (s HistoryRetention) ReconcileDefaults(ctx context.Context) error {
	return s.Repository.ReconcileDefaults(ctx)
}

func (s HistoryRetention) Days(ctx context.Context, key string, fallback int) (int, error) {
	return s.Repository.RetentionDays(ctx, key, fallback, RetentionMaxDays)
}

type HistoryPolicy struct {
	Target      HistoryTarget
	Key         string
	DefaultDays int
}

func HistoryPolicies() []HistoryPolicy {
	return []HistoryPolicy{
		{AuditLogs, AuditLogRetentionKey, 180}, {Tasks, TaskRetentionKey, 90},
		{NodeOperations, OperationRetentionKey, 90}, {ProtocolDeployments, OperationRetentionKey, 90},
		{ArchivedFlowCursors, ArchiveCursorRetentionKey, 7},
		{PeriodicSuccess, PeriodicSuccessRetentionKey, 7}, {PeriodicFailure, PeriodicFailureRetentionKey, 30},
	}
}
func (s HistoryRetention) cutoff(ctx context.Context, now time.Time, target HistoryTarget) (time.Time, error) {
	for _, policy := range HistoryPolicies() {
		if policy.Target == target {
			days, err := s.Days(ctx, policy.Key, policy.DefaultDays)
			if err != nil || days == 0 {
				return time.Time{}, err
			}
			return now.AddDate(0, 0, -days), nil
		}
	}
	return time.Time{}, errors.New("unknown retention policy")
}
func (s HistoryRetention) ReadyTarget(ctx context.Context, now time.Time, target HistoryTarget) (bool, error) {
	cutoff, err := s.cutoff(ctx, now, target)
	if err != nil || cutoff.IsZero() {
		return false, err
	}
	return s.Repository.HasPendingHistory(ctx, target, cutoff)
}
func (s HistoryRetention) RunTarget(ctx context.Context, now time.Time, target HistoryTarget) (int64, error) {
	cutoff, err := s.cutoff(ctx, now, target)
	if err != nil || cutoff.IsZero() {
		return 0, err
	}
	if target == PeriodicSuccess || target == PeriodicFailure {
		return s.Repository.PrunePeriodicRuns(ctx, cutoff, target == PeriodicSuccess)
	}
	return s.Repository.Prune(ctx, target, cutoff, HistoryBatchSize)
}

// Run is the explicit bounded maintenance entrypoint. Production schedules each
// target independently, so a slow/failed policy cannot starve another target.
func (s HistoryRetention) Run(ctx context.Context, now time.Time) (HistoryRetentionResult, error) {
	result := HistoryRetentionResult{}
	counts := map[HistoryTarget]*int64{AuditLogs: &result.AuditLogs, Tasks: &result.Tasks, NodeOperations: &result.NodeOperations, ProtocolDeployments: &result.ProtocolDeployments, ArchivedFlowCursors: &result.ArchivedFlowCursors, PeriodicSuccess: &result.PeriodicSucceeded, PeriodicFailure: &result.PeriodicFailed}
	for _, policy := range HistoryPolicies() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n, err := s.RunTarget(ctx, now, policy.Target)
		if err != nil {
			return result, err
		}
		*counts[policy.Target] = n
		if n == HistoryBatchSize {
			result.FullBatches = append(result.FullBatches, policy.Target)
		}
	}
	return result, nil
}
