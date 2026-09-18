package observability

import (
	"context"
	"time"
)

const (
	AuditLogRetentionKey  = "audit_log_retention_days"
	OperationRetentionKey = "operation_history_retention_days"
	TaskRetentionKey      = "task_history_retention_days"
	RetentionMaxDays      = 3650
	HistoryBatchSize      = 1000
)

type HistoryTarget string

const (
	AuditLogs           HistoryTarget = "audit_logs"
	Tasks               HistoryTarget = "tasks"
	NodeOperations      HistoryTarget = "node_operations"
	ProtocolDeployments HistoryTarget = "protocol_deployments"
)

type HistoryRetentionResult struct {
	AuditLogs, Tasks, NodeOperations, ProtocolDeployments int64
}

type HistoryRetentionRepository interface {
	ReconcileDefaults(context.Context) error
	RetentionDays(context.Context, string, int, int) (int, error)
	Prune(context.Context, HistoryTarget, time.Time, int) (int64, error)
	PrunePeriodicRuns(context.Context, time.Time) (int64, error)
}

type HistoryRetention struct{ Repository HistoryRetentionRepository }

func (s HistoryRetention) ReconcileDefaults(ctx context.Context) error {
	return s.Repository.ReconcileDefaults(ctx)
}

func (s HistoryRetention) Days(ctx context.Context, key string, fallback int) (int, error) {
	return s.Repository.RetentionDays(ctx, key, fallback, RetentionMaxDays)
}

func (s HistoryRetention) Run(ctx context.Context, now time.Time) (HistoryRetentionResult, error) {
	result := HistoryRetentionResult{}
	policies := []struct {
		target   HistoryTarget
		key      string
		fallback int
		count    *int64
	}{
		{AuditLogs, AuditLogRetentionKey, 180, &result.AuditLogs},
		{Tasks, TaskRetentionKey, 90, &result.Tasks},
		{NodeOperations, OperationRetentionKey, 90, &result.NodeOperations},
		{ProtocolDeployments, OperationRetentionKey, 90, &result.ProtocolDeployments},
	}
	for _, policy := range policies {
		days, err := s.Days(ctx, policy.key, policy.fallback)
		if err != nil {
			return result, err
		}
		if days == 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -days)
		for {
			count, err := s.Repository.Prune(ctx, policy.target, cutoff, HistoryBatchSize)
			if err != nil {
				return result, err
			}
			*policy.count += count
			if count < HistoryBatchSize {
				break
			}
		}
	}
	days, err := s.Days(ctx, TaskRetentionKey, 90)
	if err != nil {
		return result, err
	}
	if days > 0 {
		cutoff := now.AddDate(0, 0, -days)
		for {
			count, err := s.Repository.PrunePeriodicRuns(ctx, cutoff)
			if err != nil {
				return result, err
			}
			result.Tasks += count
			if count < HistoryBatchSize {
				break
			}
		}
	}
	return result, nil
}
