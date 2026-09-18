package jobstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type AdminRuntimeQueue struct{ DB *gorm.DB }

func (s AdminRuntimeQueue) Summary(ctx context.Context, now time.Time) (out jobs.RuntimeQueueSummary, err error) {
	err = s.DB.WithContext(ctx).Model(&model.Task{}).Where("status IN ?", []int16{0, 1, 3}).Select(`
 COALESCE(SUM(CASE WHEN status = 0 AND scheduled_at IS NOT NULL AND scheduled_at <= ? THEN 1 ELSE 0 END),0) AS pending,
 COALESCE(SUM(CASE WHEN status = 1 AND locked_until > ? THEN 1 ELSE 0 END),0) AS running,
 COALESCE(SUM(CASE WHEN status = 0 AND scheduled_at > ? THEN 1 ELSE 0 END),0) AS delayed,
 COALESCE(SUM(CASE WHEN status = 1 AND (locked_until IS NULL OR locked_until <= ?) THEN 1 ELSE 0 END),0) AS stale,
 COALESCE(SUM(CASE WHEN status = 0 AND scheduled_at IS NULL THEN 1 ELSE 0 END),0) AS drafts,
 COALESCE(SUM(CASE WHEN status = 3 THEN 1 ELSE 0 END),0) AS failed`, now, now, now, now).Scan(&out).Error
	out.ID, out.Name = jobs.AdminRuntimeQueue, "运营任务"
	return out, err
}

func (s AdminRuntimeQueue) Page(ctx context.Context, now time.Time, limit, offset int) (out jobs.RuntimeQueuePage, err error) {
	q := s.DB.WithContext(ctx).Model(&model.Task{}).Where("status IN ?", []int16{0, 1, 3})
	if err = q.Count(&out.Total).Error; err != nil {
		return out, err
	}
	var rows []model.Task
	if err = q.Select("id,type,status,attempts,created_at,scheduled_at,locked_until,errors").Order("status ASC, priority DESC, id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return out, err
	}
	out.Items = make([]jobs.RuntimeQueueItem, 0, len(rows))
	for _, row := range rows {
		state := "pending"
		switch {
		case row.Status == 3:
			state = "failed"
		case row.Status == 1:
			state = "running"
			if row.LockedUntil == nil || !row.LockedUntil.After(now) {
				state = "stale"
			}
		case row.ScheduledAt == nil:
			state = "draft"
		case row.ScheduledAt.After(now):
			state = "delayed"
		}
		out.Items = append(out.Items, jobs.RuntimeQueueItem{ID: row.ID, Kind: row.Type, State: state, Attempts: row.Attempts, CreatedAt: row.CreatedAt, NextAttemptAt: row.ScheduledAt, LeaseUntil: row.LockedUntil, LastError: row.Errors})
	}
	return out, nil
}
