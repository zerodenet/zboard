package entitlementstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
)

type QuotaRequests struct{ DB *gorm.DB }

func (s QuotaRequests) Create(ctx context.Context, actor uint, in entitlements.QuotaRequestInput) (jobs.BatchReceipt, error) {
	var out jobs.BatchReceipt
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		if err := subscriptionReader(tx, actor, true); err != nil {
			return err
		}
		var admin model.User
		if err := tx.Select("id", "email").First(&admin, actor).Error; err != nil {
			return err
		}
		query := tx.Model(&model.Subscription{}).Where("end_at > ?", time.Now().UTC())
		if !in.Scope.AllActive {
			switch {
			case len(in.Scope.SubscriptionIDs) > 0 && len(in.Scope.UserIDs) > 0:
				query = query.Where("id IN ? OR user_id IN ?", in.Scope.SubscriptionIDs, in.Scope.UserIDs)
			case len(in.Scope.SubscriptionIDs) > 0:
				query = query.Where("id IN ?", in.Scope.SubscriptionIDs)
			default:
				query = query.Where("user_id IN ?", in.Scope.UserIDs)
			}
		}
		var ids []uint
		if err := query.Order("id").Limit(entitlements.MaxQuotaTargets+1).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) > entitlements.MaxQuotaTargets {
			return entitlements.ErrQuotaRequestInvalid
		}
		scope, err := json.Marshal(in.Scope)
		if err != nil {
			return err
		}
		content, err := json.Marshal(in.Content)
		if err != nil {
			return err
		}
		task := model.Task{Type: "quota", Scope: string(scope), Content: string(content), Total: int64(len(ids)), IdempotencyKey: in.IdempotencyKey, Priority: in.Priority, MaxAttempts: in.MaxAttempts}
		if in.AutoRun {
			now := time.Now().UTC()
			task.ScheduledAt = &now
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		items := make([]model.TaskItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, model.TaskItem{TaskID: task.ID, TargetType: "subscription", TargetID: strconv.FormatUint(uint64(id), 10), Payload: "{}"})
		}
		if err := tx.CreateInBatches(items, 250).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "task.create", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("type=quota total=%d", task.Total)}).Error; err != nil {
			return err
		}
		out = jobs.BatchReceipt{ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status, Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey, Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate entry") {
			return jobs.BatchReceipt{}, entitlements.ErrQuotaRequestConflict
		}
		return jobs.BatchReceipt{}, err
	}
	return out, nil
}
