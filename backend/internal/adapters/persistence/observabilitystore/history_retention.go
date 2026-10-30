package observabilitystore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HistoryRetention struct{ DB *gorm.DB }

func (s HistoryRetention) PrunePeriodicRuns(ctx context.Context, cutoff time.Time, successful bool) (int64, error) {
	return jobstore.New(s.DB).PrunePeriodicOutcome(ctx, cutoff, successful)
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
	if limit < 1 || limit > observability.HistoryBatchSize {
		return 0, errors.New("invalid history batch")
	}
	if target == observability.ArchivedFlowCursors {
		return s.pruneArchivedFlowCursors(ctx, cutoff, limit)
	}
	query, value, column, task, err := historyQuery(s.DB.WithContext(ctx), target, cutoff)
	if err != nil {
		return 0, err
	}
	var candidates []uint
	if err := query.Order(column+",id").Limit(limit).Pluck("id", &candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		locked, _, _, _, err := historyQuery(tx, target, cutoff)
		if err != nil {
			return err
		}
		if err := locked.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", candidates).Order("id").Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if task {
			return pruneTaskHistoryChildren(tx, ids, limit, &count)
		}
		deleted := tx.Where("id IN ?", ids).Delete(value)
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

func historyQuery(db *gorm.DB, target observability.HistoryTarget, cutoff time.Time) (*gorm.DB, any, string, bool, error) {
	value, column, task, err := historyTarget(target)
	if err != nil {
		return nil, nil, "", false, err
	}
	q := db.Model(value).Where(column+" < ?", cutoff)
	if task {
		q = q.Where("status IN ?", []int16{2, 3}).Where("NOT EXISTS (?)", db.Model(&model.MailDeliveryAttempt{}).Select("1").Where("mail_delivery_attempts.task_id = tasks.id AND (acceptance = ? OR finished_at IS NULL)", "unknown")).Where("NOT EXISTS (?)", db.Model(&model.TaskItem{}).Select("1").Where("task_items.task_id = tasks.id AND task_items.status = 1"))
	}
	if target == observability.NodeOperations || target == observability.ProtocolDeployments {
		q = q.Where("status IN ?", []string{"succeeded", "failed"})
	}
	return q, value, column, task, nil
}
func (s HistoryRetention) HasPendingHistory(ctx context.Context, target observability.HistoryTarget, cutoff time.Time) (bool, error) {
	if target == observability.PeriodicSuccess || target == observability.PeriodicFailure {
		return jobstore.New(s.DB).HasPrunablePeriodic(ctx, cutoff, target == observability.PeriodicSuccess)
	}
	var ids []uint
	if target == observability.ArchivedFlowCursors {
		err := archivedCursorNodes(s.DB.WithContext(ctx), cutoff).Order("archived_at,id").Limit(1).Pluck("id", &ids).Error
		return len(ids) > 0, err
	}
	q, _, column, _, err := historyQuery(s.DB.WithContext(ctx), target, cutoff)
	if err != nil {
		return false, err
	}
	err = q.Order(column+",id").Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
}
