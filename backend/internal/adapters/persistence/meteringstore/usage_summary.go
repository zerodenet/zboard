package meteringstore

import (
	"context"
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type UsageSummary struct{ DB, ReadDB *gorm.DB }

func (s UsageSummary) Read(ctx context.Context, actor uint, q metering.UsageSummaryQuery) (metering.UsageSummarySnapshot, error) {
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.UsageSummarySnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	now := time.Now().UTC()
	result := metering.UsageSummarySnapshot{ScopeUser: q.UserID, AsOf: now.Format(time.RFC3339)}
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	err := read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scope := func(query *gorm.DB) *gorm.DB {
			if q.UserID > 0 {
				return query.Where("user_id = ?", q.UserID)
			}
			return query
		}
		if err := scope(tx.Model(&model.TrafficRecord{})).Select("COALESCE(SUM(used_bytes), 0)").Scan(&result.TotalUsedBytes).Error; err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scope(tx.Model(&model.Subscription{})).Where("status = ? AND end_at > ? AND flow_used < flow_total", "active", now).Select("COALESCE(SUM(flow_total - flow_used), 0)").Scan(&result.RemainingBytes).Error; err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return scope(tx.Model(&model.TrafficRecord{})).Where("record_at >= ?", now.Truncate(24*time.Hour)).Select("COALESCE(SUM(used_bytes), 0)").Scan(&result.UsedBytesToday).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.UsageSummarySnapshot{}, err
	}
	return result, nil
}
