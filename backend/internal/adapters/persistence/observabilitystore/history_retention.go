package observabilitystore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HistoryRetention struct{ DB *gorm.DB }

func (s HistoryRetention) PrunePeriodicRuns(ctx context.Context, cutoff time.Time) (int64, error) {
	return jobstore.New(s.DB).PrunePeriodic(ctx, cutoff)
}

func (s HistoryRetention) ReconcileDefaults(ctx context.Context) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, definition := range platformstore.SetupPreferenceDefinitions() {
			var existing model.SystemConfig
			err := tx.Where("config_key = ?", definition.ConfigKey).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if err := tx.Create(&definition).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				if err := tx.Model(&existing).Updates(map[string]any{"name": definition.Name, "value_type": definition.ValueType, "description": definition.Description, "is_public": definition.IsPublic, "is_secret": definition.IsSecret}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s HistoryRetention) RetentionDays(ctx context.Context, key string, fallback, maximum int) (int, error) {
	var config model.SystemConfig
	if err := s.DB.WithContext(ctx).Select("value").Where("config_key = ?", key).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fallback, nil
		}
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(config.Value))
	if err != nil || value < 0 || value > maximum {
		return fallback, nil
	}
	return value, nil
}

func (s HistoryRetention) Prune(ctx context.Context, target observability.HistoryTarget, cutoff time.Time, limit int) (count int64, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		modelValue, column, task, err := historyTarget(target)
		if err != nil {
			return err
		}
		var ids []uint
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(modelValue).Where(column+" < ?", cutoff)
		if task {
			query = query.Where("status IN ?", []int16{2, 3})
		}
		if err := query.Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if task {
			if err := messagingstore.PruneMailAttempts(tx, ids); err != nil {
				return err
			}
			if err := tx.Where("task_id IN ?", ids).Delete(&model.TaskItem{}).Error; err != nil {
				return err
			}
		}
		deleted := tx.Where("id IN ?", ids).Delete(modelValue)
		count = deleted.RowsAffected
		return deleted.Error
	})
	return
}

func historyTarget(target observability.HistoryTarget) (any, string, bool, error) {
	switch target {
	case observability.AuditLogs:
		return &model.AuditLog{}, "created_at", false, nil
	case observability.Tasks:
		return &model.Task{}, "finished_at", true, nil
	case observability.NodeOperations:
		return &model.NodeOperation{}, "finished_at", false, nil
	case observability.ProtocolDeployments:
		return &model.ProtocolDeployment{}, "finished_at", false, nil
	default:
		return nil, "", false, errors.New("unknown history retention target")
	}
}
