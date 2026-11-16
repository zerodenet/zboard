package application

import (
	"context"
	"log"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
)

func (s *Services) HistoryRetentionJobs() []PeriodicJob {
	targets := []struct {
		id, name string
		target   observability.HistoryTarget
		interval time.Duration
	}{
		{"history_retention", "审计历史保留", observability.AuditLogs, time.Minute},
		{"history_retention_tasks", "运营任务历史保留", observability.Tasks, time.Minute},
		{"history_retention_node_operations", "节点操作历史保留", observability.NodeOperations, time.Minute},
		{"history_retention_protocol_deployments", "发布历史保留", observability.ProtocolDeployments, time.Minute},
		{"history_retention_archived_cursors", "已删除节点计费游标回收", observability.ArchivedFlowCursors, time.Minute},
		{"history_retention_periodic_success", "成功周期任务历史保留", observability.PeriodicSuccess, time.Minute},
		{"history_retention_periodic_failure", "失败周期任务历史保留", observability.PeriodicFailure, time.Minute},
	}
	jobs := make([]PeriodicJob, 0, len(targets)+2)
	for _, target := range targets {
		jobs = append(jobs, PeriodicJob{ID: target.id, Name: target.name, Interval: target.interval, Timeout: 20 * time.Second, Immediate: true, RetryFailures: true, ReconcileAfterLoss: true, CompactHistory: true,
			Ready: func(ctx context.Context) (bool, error) {
				now := time.Now().UTC()
				due, err := s.HistoryRetention.CleanupDue(ctx, now)
				if err != nil || !due {
					return false, err
				}
				return s.HistoryRetention.ReadyTarget(ctx, now, target.target)
			},
			Run: func(ctx context.Context) error {
				n, err := s.HistoryRetention.RunTarget(ctx, time.Now().UTC(), target.target)
				if n > 0 && err == nil {
					log.Printf("retention batch target=%s rows=%d limit=%d", target.target, n, observability.HistoryBatchSize)
				}
				return err
			},
		})
	}
	for _, operation := range []struct {
		id, name string
		kind     network.OperationKind
	}{{"history_retention_certificates", "证书操作历史保留", network.CertificateOperation}, {"history_retention_dns", "DNS 操作历史保留", network.DNSOperation}} {
		cutoff := func(ctx context.Context) (time.Time, error) {
			days, err := s.HistoryRetention.Days(ctx, observability.OperationRetentionKey, observability.DefaultOperationRetentionDays)
			if err != nil || days == 0 {
				return time.Time{}, err
			}
			return time.Now().UTC().AddDate(0, 0, -days), nil
		}
		jobs = append(jobs, PeriodicJob{ID: operation.id, Name: operation.name, Interval: time.Minute, Timeout: 20 * time.Second, Immediate: true, RetryFailures: true, ReconcileAfterLoss: true, CompactHistory: true,
			Ready: func(ctx context.Context) (bool, error) {
				due, err := s.HistoryRetention.CleanupDue(ctx, time.Now().UTC())
				if err != nil || !due {
					return false, err
				}
				before, err := cutoff(ctx)
				if err != nil || before.IsZero() {
					return false, err
				}
				return s.OperationHistory.HasPending(ctx, operation.kind, before)
			},
			Run: func(ctx context.Context) error {
				before, err := cutoff(ctx)
				if err != nil || before.IsZero() {
					return err
				}
				_, err = s.OperationHistory.Prune(ctx, operation.kind, before)
				return err
			},
		})
	}
	return jobs
}
