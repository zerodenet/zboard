package observabilitystore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type AuditDirectory struct{ DB *gorm.DB }

func auditLogRecord(r model.AuditLog) observability.AuditLogRecord {
	return observability.AuditLogRecord{ID: r.ID, UserID: r.UserID, Actor: r.Actor, Action: r.Action, Target: r.Target, Detail: r.Detail, CreatedAt: r.CreatedAt}
}

func (s AuditDirectory) ListAuditLogs(ctx context.Context, input observability.AuditLogQuery) (observability.AuditLogPage, error) {
	q := s.DB.WithContext(ctx).Model(&model.AuditLog{}).Where("created_at >= ? AND created_at < ?", input.From, input.To)
	if input.Actor != "" {
		q = q.Where("actor = ?", input.Actor)
	}
	if input.Action != "" {
		q = q.Where("action = ?", input.Action)
	}
	if input.Target != "" {
		q = q.Where("target = ?", input.Target)
	}
	page := observability.AuditLogPage{}
	if err := q.Count(&page.Total).Error; err != nil {
		return page, err
	}
	if input.CursorID == 0 && input.Offset > 0 {
		q = q.Order("created_at desc, id desc").Offset(input.Offset).Limit(input.Limit)
	} else {
		if input.CursorID != 0 {
			if input.CursorDirection == "older" {
				q = q.Where("(created_at < ?) OR (created_at = ? AND id < ?)", input.CursorAt, input.CursorAt, input.CursorID)
			} else {
				q = q.Where("(created_at > ?) OR (created_at = ? AND id > ?)", input.CursorAt, input.CursorAt, input.CursorID)
			}
		}
		order := "created_at desc, id desc"
		if input.CursorID != 0 && input.CursorDirection == "newer" {
			order = "created_at asc, id asc"
		}
		q = q.Order(order).Limit(input.Limit + 1)
	}
	var rows []model.AuditLog
	if err := q.Find(&rows).Error; err != nil {
		return page, err
	}
	if len(rows) > input.Limit {
		page.HasMore = true
		rows = rows[:input.Limit]
	}
	if input.CursorID != 0 && input.CursorDirection == "newer" {
		for l, r := 0, len(rows)-1; l < r; l, r = l+1, r-1 {
			rows[l], rows[r] = rows[r], rows[l]
		}
	}
	for _, row := range rows {
		page.Items = append(page.Items, auditLogRecord(row))
	}
	return page, nil
}
func (s AuditDirectory) AuditLog(ctx context.Context, id uint) (observability.AuditLogRecord, error) {
	var row model.AuditLog
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return observability.AuditLogRecord{}, observability.ErrAuditNotFound
		}
		return observability.AuditLogRecord{}, err
	}
	return auditLogRecord(row), nil
}
