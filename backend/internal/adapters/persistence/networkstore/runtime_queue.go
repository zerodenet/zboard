package networkstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PublicationRuntimeQueue struct{ DB *gorm.DB }

func (s PublicationRuntimeQueue) Retry(ctx context.Context, nodeID, actorID uint, actor string, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.NodeConfigPublish
		if err := tx.Where("node_id = ?", nodeID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrRuntimeQueueConflict
			}
			return err
		}
		if row.LeaseUntil.After(now) || !row.NextAttemptAt.After(now) && row.LeaseToken == "" {
			return jobs.ErrRuntimeQueueConflict
		}
		result := tx.Model(&model.NodeConfigPublish{}).
			Where("node_id = ? AND generation = ? AND lease_until <= ?", nodeID, row.Generation, now).
			Updates(map[string]any{"generation": gorm.Expr("generation + 1"), "next_attempt_at": now, "lease_token": "", "lease_until": publicationIdleLease, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return jobs.ErrRuntimeQueueConflict
		}
		return tx.Create(&model.AuditLog{UserID: &actorID, Actor: actor, Action: "node.publish.retry", Target: fmt.Sprintf("node:%d", nodeID), Detail: fmt.Sprintf("generation=%d attempts=%d", row.Generation+1, row.Attempts), CreatedAt: now}).Error
	})
}

func (s PublicationRuntimeQueue) Summary(ctx context.Context, now time.Time) (out jobs.RuntimeQueueSummary, err error) {
	err = s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{}).Select(`
 COALESCE(SUM(CASE WHEN lease_until <= ? AND next_attempt_at <= ? THEN 1 ELSE 0 END),0) AS pending,
 COALESCE(SUM(CASE WHEN lease_until > ? THEN 1 ELSE 0 END),0) AS running,
 COALESCE(SUM(CASE WHEN lease_until <= ? AND next_attempt_at > ? THEN 1 ELSE 0 END),0) AS delayed,
 COALESCE(SUM(CASE WHEN lease_token <> '' AND lease_until <= ? THEN 1 ELSE 0 END),0) AS stale,
 COALESCE(SUM(CASE WHEN last_error <> '' THEN 1 ELSE 0 END),0) AS failed,
 MIN(CASE WHEN lease_until <= ? AND next_attempt_at <= ? THEN created_at END) AS oldest_at`, now, now, now, now, now, now, now, now).Scan(&out).Error
	out.ID, out.Name = jobs.PublicationRuntimeQueue, "节点配置发布"
	return out, err
}

func (s PublicationRuntimeQueue) Page(ctx context.Context, now time.Time, limit, offset int) (out jobs.RuntimeQueuePage, err error) {
	q := s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{})
	if err = q.Count(&out.Total).Error; err != nil {
		return out, err
	}
	var rows []model.NodeConfigPublish
	if err = q.Order("next_attempt_at ASC, node_id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return out, err
	}
	out.Items = make([]jobs.RuntimeQueueItem, 0, len(rows))
	for _, row := range rows {
		state := "pending"
		switch {
		case row.LeaseUntil.After(now):
			state = "running"
		case row.LeaseToken != "":
			state = "stale"
		case row.NextAttemptAt.After(now):
			state = "delayed"
		}
		lease, next := row.LeaseUntil, row.NextAttemptAt
		out.Items = append(out.Items, jobs.RuntimeQueueItem{ID: row.NodeID, Kind: "node", State: state, Attempts: int(row.Attempts), CreatedAt: row.CreatedAt, NextAttemptAt: &next, LeaseUntil: &lease, LastError: row.LastError})
	}
	return out, nil
}
