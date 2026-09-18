package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"log"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	auditLogRetentionKey      = observability.AuditLogRetentionKey
	operationRetentionKey     = observability.OperationRetentionKey
	taskRetentionKey          = observability.TaskRetentionKey
	systemTimezoneKey         = "system_timezone"
	defaultSystemTimezone     = "UTC"
	defaultAuditRetentionDays = 180
	defaultOperationRetention = 90
	defaultTaskRetentionDays  = 90
	historyRetentionMaxDays   = observability.RetentionMaxDays
	historyRetentionInterval  = 6 * time.Hour
)

type historyRetentionResult struct {
	AuditLogs             int64
	Tasks                 int64
	NodeOperations        int64
	ProtocolDeployments   int64
	CertificateOperations int64
	ProviderOperations    int64
}

func historyRetentionDefaults() []model.SystemConfig {
	return platformstore.SetupPreferenceDefinitions()
}

func (h *handlers) ReconcileHistoryRetentionDefaults() error {
	return h.services.HistoryRetention.ReconcileDefaults(context.Background())
}

func (h *handlers) StartHistoryRetentionWorker() {
	h.startScheduledJob("history_retention", historyRetentionInterval, func(ctx context.Context) error {
		_, err := h.runHistoryRetentionContext(ctx, time.Now().UTC())
		return err
	})
}

func (h *handlers) CloseHistoryRetentionWorker() { h.closeScheduledJob("history_retention") }

func (h *handlers) runHistoryRetentionSafely() {
	if h.backgroundWorkPaused() {
		return
	}
	result, err := h.runHistoryRetention(time.Now().UTC())
	if err != nil {
		log.Printf("history retention cleanup failed: %v", err)
		return
	}
	if result.AuditLogs+result.Tasks+result.NodeOperations+result.ProtocolDeployments+result.CertificateOperations+result.ProviderOperations > 0 {
		log.Printf("history retention cleanup completed: audit_logs=%d tasks=%d node_operations=%d protocol_deployments=%d certificate_operations=%d provider_operations=%d",
			result.AuditLogs, result.Tasks, result.NodeOperations, result.ProtocolDeployments, result.CertificateOperations, result.ProviderOperations)
	}
}

func (h *handlers) runHistoryRetention(now time.Time) (historyRetentionResult, error) {
	return h.runHistoryRetentionContext(context.Background(), now)
}
func (h *handlers) runHistoryRetentionContext(ctx context.Context, now time.Time) (historyRetentionResult, error) {
	core, err := h.services.HistoryRetention.Run(ctx, now)
	result := historyRetentionResult{AuditLogs: core.AuditLogs, Tasks: core.Tasks, NodeOperations: core.NodeOperations, ProtocolDeployments: core.ProtocolDeployments}
	if err != nil {
		return result, err
	}
	days := h.historyRetentionDays(operationRetentionKey, defaultOperationRetention)
	if days != 0 {
		for kind, count := range map[network.OperationKind]*int64{network.CertificateOperation: &result.CertificateOperations, network.DNSOperation: &result.ProviderOperations} {
			for {
				n, err := h.services.OperationHistory.Prune(ctx, kind, now.AddDate(0, 0, -days))
				if err != nil {
					return result, err
				}
				*count += n
				if n < network.HistoryBatchSize {
					break
				}
			}
		}
	}
	return result, nil
}

func (h *handlers) historyRetentionDays(key string, fallback int) int {
	value, err := h.services.HistoryRetention.Days(context.Background(), key, fallback)
	if err != nil {
		return fallback
	}
	return value
}
