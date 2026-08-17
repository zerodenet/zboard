package handler

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	auditLogRetentionKey      = "audit_log_retention_days"
	operationRetentionKey     = "operation_history_retention_days"
	taskRetentionKey          = "task_history_retention_days"
	defaultAuditRetentionDays = 180
	defaultOperationRetention = 90
	defaultTaskRetentionDays  = 90
	historyRetentionMaxDays   = 3650
	historyRetentionInterval  = 6 * time.Hour
)

type historyRetentionRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type historyRetentionResult struct {
	AuditLogs             int64
	Tasks                 int64
	NodeOperations        int64
	ProtocolDeployments   int64
	CertificateOperations int64
	ProviderOperations    int64
}

var historyRetentionRegistry sync.Map

func historyRetentionDefaults() []model.SystemConfig {
	return []model.SystemConfig{
		{
			ConfigKey:   auditLogRetentionKey,
			Name:        "审计日志保留天数",
			Value:       strconv.Itoa(defaultAuditRetentionDays),
			ValueType:   "int",
			Description: "超过该天数的审计日志会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
		{
			ConfigKey:   operationRetentionKey,
			Name:        "运行历史保留天数",
			Value:       strconv.Itoa(defaultOperationRetention),
			ValueType:   "int",
			Description: "超过该天数且已结束的节点操作、协议发布、证书与供应商操作会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
		{
			ConfigKey:   taskRetentionKey,
			Name:        "运营任务保留天数",
			Value:       strconv.Itoa(defaultTaskRetentionDays),
			ValueType:   "int",
			Description: "超过该天数且已完成的运营任务及任务项会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
	}
}

func (h *handlers) ReconcileHistoryRetentionDefaults() error {
	for _, definition := range historyRetentionDefaults() {
		var existing model.SystemConfig
		err := h.db.Where("config_key = ?", definition.ConfigKey).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := h.db.Create(&definition).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := h.db.Model(&existing).Updates(map[string]interface{}{
				"name":        definition.Name,
				"value_type":  definition.ValueType,
				"description": definition.Description,
				"is_public":   false,
				"is_secret":   false,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *handlers) StartHistoryRetentionWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &historyRetentionRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := historyRetentionRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		h.runHistoryRetentionSafely()
		ticker := time.NewTicker(historyRetentionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.runHistoryRetentionSafely()
			}
		}
	}()
}

func (h *handlers) CloseHistoryRetentionWorker() {
	value, ok := historyRetentionRegistry.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*historyRetentionRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) runHistoryRetentionSafely() {
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
	auditDays := h.historyRetentionDays(auditLogRetentionKey, defaultAuditRetentionDays)
	operationDays := h.historyRetentionDays(operationRetentionKey, defaultOperationRetention)
	taskDays := h.historyRetentionDays(taskRetentionKey, defaultTaskRetentionDays)
	result := historyRetentionResult{}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if auditDays > 0 {
			deleted := tx.Where("created_at < ?", now.AddDate(0, 0, -auditDays)).Delete(&model.AuditLog{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.AuditLogs = deleted.RowsAffected
		}

		if taskDays > 0 {
			cutoff := now.AddDate(0, 0, -taskDays)
			taskIDs := tx.Model(&model.Task{}).Select("id").Where("finished_at IS NOT NULL AND finished_at < ?", cutoff)
			if err := tx.Where("task_id IN (?)", taskIDs).Delete(&model.TaskItem{}).Error; err != nil {
				return err
			}
			deleted := tx.Where("finished_at IS NOT NULL AND finished_at < ?", cutoff).Delete(&model.Task{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.Tasks = deleted.RowsAffected
		}

		if operationDays > 0 {
			cutoff := now.AddDate(0, 0, -operationDays)
			for _, target := range []struct {
				model interface{}
				count *int64
			}{
				{&model.NodeOperation{}, &result.NodeOperations},
				{&model.ProtocolDeployment{}, &result.ProtocolDeployments},
				{&model.CertificateOperation{}, &result.CertificateOperations},
				{&model.ProviderOperation{}, &result.ProviderOperations},
			} {
				deleted := tx.Where("finished_at IS NOT NULL AND finished_at < ?", cutoff).Delete(target.model)
				if deleted.Error != nil {
					return deleted.Error
				}
				*target.count = deleted.RowsAffected
			}
		}
		return nil
	})
	return result, err
}

func (h *handlers) historyRetentionDays(key string, fallback int) int {
	var config model.SystemConfig
	if err := h.db.Select("value").Where("config_key = ?", key).First(&config).Error; err != nil {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(config.Value))
	if err != nil || value < 0 || value > historyRetentionMaxDays {
		return fallback
	}
	return value
}
