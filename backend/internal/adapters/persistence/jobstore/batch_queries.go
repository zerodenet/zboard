package jobstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BatchQueries struct{ DB *gorm.DB }

func batchReader(tx *gorm.DB, actor uint) error {
	var user model.User
	res := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").Limit(1).Find(&user)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return jobs.ErrBatchPermission
	}
	return nil
}
func (s BatchQueries) List(ctx context.Context, actor uint, f jobs.BatchFilter) (jobs.BatchPage, error) {
	out := jobs.BatchPage{Items: []jobs.BatchView{}, Limit: f.Limit, Offset: f.Offset}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := batchReader(tx, actor); err != nil {
			return err
		}
		query := tx.Model(&model.Task{})
		if f.Type != "" {
			query = query.Where("type = ?", f.Type)
		}
		if f.Status != nil {
			query = query.Where("status = ?", *f.Status)
		}
		if err := query.Count(&out.Total).Error; err != nil {
			return err
		}
		// Scope aggregate work to the bounded requested page, not the entire history.
		var ids []uint
		if err := query.Order("priority DESC, id DESC").Offset(f.Offset).Limit(f.Limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return batchProjectionFor(tx, ids).Order("tasks.priority DESC, tasks.id DESC").Scan(&out.Items).Error
	})
	if err != nil {
		return jobs.BatchPage{}, err
	}
	return out, nil
}
func batchProjectionFor(tx *gorm.DB, ids []uint) *gorm.DB {
	counts := tx.Model(&model.TaskItem{}).Where("task_id IN ?", ids).Select("task_id, SUM(CASE WHEN status=0 THEN 1 ELSE 0 END) AS pending_count, SUM(CASE WHEN status=1 THEN 1 ELSE 0 END) AS running_count, SUM(CASE WHEN status=2 THEN 1 ELSE 0 END) AS succeeded_count, SUM(CASE WHEN status=3 THEN 1 ELSE 0 END) AS failed_count").Group("task_id")
	return tx.Model(&model.Task{}).Where("tasks.id IN ?", ids).Joins("LEFT JOIN (?) AS item_counts ON item_counts.task_id = tasks.id", counts).Select("tasks.*, COALESCE(item_counts.pending_count,0) AS pending_count, COALESCE(item_counts.running_count,0) AS running_count, COALESCE(item_counts.succeeded_count,0) AS succeeded_count, COALESCE(item_counts.failed_count,0) AS failed_count")
}
func (s BatchQueries) Detail(ctx context.Context, actor, id uint, items bool) (jobs.BatchDetail, error) {
	var out jobs.BatchDetail
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := batchReader(tx, actor); err != nil {
			return err
		}
		res := batchProjectionFor(tx, []uint{id}).Scan(&out.BatchView)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return jobs.ErrBatchNotFound
		}
		if items {
			return tx.Model(&model.TaskItem{}).Where("task_id = ?", id).Order("id").Limit(10000).Find(&out.Items).Error
		}
		return nil
	})
	if err != nil {
		return jobs.BatchDetail{}, err
	}
	return out, nil
}
func (s BatchQueries) Items(ctx context.Context, actor, id uint, f jobs.BatchFilter) (jobs.BatchItemPage, error) {
	out := jobs.BatchItemPage{Items: []jobs.BatchItem{}, Limit: f.Limit, Offset: f.Offset}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := batchReader(tx, actor); err != nil {
			return err
		}
		var exists int64
		if err := tx.Model(&model.Task{}).Where("id = ?", id).Count(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			return jobs.ErrBatchNotFound
		}
		q := tx.Model(&model.TaskItem{}).Where("task_id = ?", id)
		if f.Status != nil {
			q = q.Where("status = ?", *f.Status)
		}
		if err := q.Count(&out.Total).Error; err != nil {
			return err
		}
		return q.Order("id").Offset(f.Offset).Limit(f.Limit).Find(&out.Items).Error
	})
	if err != nil {
		return jobs.BatchItemPage{}, err
	}
	return out, nil
}
func (s BatchQueries) Summary(ctx context.Context, actor uint) (jobs.BatchSummary, error) {
	var out jobs.BatchSummary
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := batchReader(tx, actor); err != nil {
			return err
		}
		if err := tx.Model(&model.Task{}).Select("COUNT(*) AS total, COALESCE(SUM(status=0),0) AS pending, COALESCE(SUM(status=1),0) AS running, COALESCE(SUM(status=2),0) AS completed, COALESCE(SUM(status=3),0) AS failed, COALESCE(SUM(CASE WHEN status IN (0,1) THEN current ELSE 0 END),0) AS active_current, COALESCE(SUM(CASE WHEN status IN (0,1) THEN total ELSE 0 END),0) AS active_total").Scan(&out).Error; err != nil {
			return err
		}
		var targets jobs.BatchSummary
		if err := tx.Model(&model.TaskItem{}).Select("COALESCE(SUM(status=0),0) AS pending_targets, COALESCE(SUM(status=1),0) AS running_targets, COALESCE(SUM(status=2),0) AS succeeded_targets, COALESCE(SUM(status=3),0) AS failed_targets").Scan(&targets).Error; err != nil {
			return err
		}
		out.PendingTargets = targets.PendingTargets
		out.RunningTargets = targets.RunningTargets
		out.SucceededTargets = targets.SucceededTargets
		out.FailedTargets = targets.FailedTargets
		return nil
	})
	if err != nil {
		return jobs.BatchSummary{}, err
	}
	return out, nil
}
