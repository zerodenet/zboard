package messagingstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"strings"
	"time"
)

type Requests struct{ DB *gorm.DB }

func (s Requests) Create(ctx context.Context, actor uint, in messaging.RequestInput) (messaging.RequestReceipt, error) {
	var out messaging.RequestReceipt
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := templateAdministrator(tx, actor)
		if err != nil {
			return err
		}
		if in.Content.TemplateID != 0 {
			var template model.EmailTemplate
			res := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_active = ?", in.Content.TemplateID, true).Limit(1).Find(&template)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 || template.Revision != in.Content.TemplateRevision || template.SubjectTemplate != in.Content.Subject || template.BodyTemplate != in.Content.Body {
				return messaging.ErrInvalidMessage
			}
		} else {
			in.Content.TemplateRevision = 0
		}
		var site model.Installation
		if err := tx.First(&site, 1).Error; err != nil {
			return err
		}
		in.Content.SiteName = strings.TrimSpace(site.SiteName)
		if in.Content.SiteName == "" {
			in.Content.SiteName = "Zboard"
		}
		in.Content.SiteURL = strings.TrimSpace(site.SiteURL)
		query := tx.Model(&model.User{}).Where("status = ?", "active")
		if !in.Scope.AllActive {
			query = query.Where("id IN ?", in.Scope.UserIDs)
		}
		var ids []uint
		if err := query.Order("id").Limit(messaging.MaxRequestRecipients+1).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) > messaging.MaxRequestRecipients {
			return messaging.ErrInvalidMessage
		}
		scope, err := json.Marshal(in.Scope)
		if err != nil {
			return err
		}
		content, err := json.Marshal(in.Content)
		if err != nil {
			return err
		}
		task := model.Task{Type: "email", Scope: string(scope), Content: string(content), Total: int64(len(ids)), IdempotencyKey: in.IdempotencyKey, Priority: in.Priority, MaxAttempts: in.MaxAttempts}
		if in.AutoRun {
			now := time.Now().UTC()
			task.ScheduledAt = &now
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		items := make([]model.TaskItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: strconv.FormatUint(uint64(id), 10), Payload: "{}"})
		}
		if err := tx.CreateInBatches(items, 250).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "task.create", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("type=email total=%d", task.Total)}).Error; err != nil {
			return err
		}
		out = messaging.RequestReceipt{ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status, Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey, Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate entry") {
			return messaging.RequestReceipt{}, messaging.ErrRequestConflict
		}
		return messaging.RequestReceipt{}, err
	}
	return out, nil
}
