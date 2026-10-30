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
		{"history_retention", "审计历史保留", observability.AuditLogs, 5 * time.Minute},
		{"history_retention_tasks", "运营任务历史保留", observability.Tasks, 5 * time.Minute},
		{"history_retention_node_operations", "节点操作历史保留", observability.NodeOperations, 5 * time.Minute},
		{"history_retention_protocol_deployments", "发布历史保留", observability.ProtocolDeployments, 5 * time.Minute},
		{"history_retention_archived_cursors", "已删除节点计费游标回收", observability.ArchivedFlowCursors, time.Minute},
		{"history_retention_periodic_success", "成功周期任务历史保留", observability.PeriodicSuccess, time.Minute},
		{"history_retention_periodic_failure", "失败周期任务历史保留", observability.PeriodicFailure, time.Minute},
	}
	jobs := make([]PeriodicJob, 0, len(targets)+2)
	for _, target := range targets {
		jobs = append(jobs, PeriodicJob{ID: target.id, Name: target.name, Interval: target.interval, Timeout: 20 * time.Second, Immediate: true, RetryFailures: true, ReconcileAfterLoss: true,
			Ready: func(ctx context.Context) (bool, error) {
				return s.HistoryRetention.ReadyTarget(ctx, time.Now().UTC(), target.target)
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
			days, err := s.HistoryRetention.Days(ctx, observability.OperationRetentionKey, 90)
			if err != nil || days == 0 {
				return time.Time{}, err
			}
			return time.Now().UTC().AddDate(0, 0, -days), nil
		}
		jobs = append(jobs, PeriodicJob{ID: operation.id, Name: operation.name, Interval: 5 * time.Minute, Timeout: 20 * time.Second, Immediate: true, RetryFailures: true, ReconcileAfterLoss: true,
			Ready: func(ctx context.Context) (bool, error) {
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
