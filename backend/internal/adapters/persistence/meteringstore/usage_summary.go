package meteringstore

import (
	"context"
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
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
		// Both windows contain whole UTC hours, including the open hour. The
		// projection is updated in the accounting transaction, so it includes
		// newly committed and late-arriving usage without scanning the ledger.
		// Preserve the existing unbounded upper edge (including future records).
		var usage struct{ TotalUsedBytes, UsedBytesToday int64 }
		if err := scope(tx.Table("traffic_usage_hourly")).Where("record_count > 0").Select(
			"COALESCE(SUM(used_bytes), 0) AS total_used_bytes, COALESCE(SUM(CASE WHEN record_at >= ? THEN used_bytes ELSE 0 END), 0) AS used_bytes_today",
			now.Truncate(24*time.Hour).Format("2006-01-02 15:04:05"),
		).Scan(&usage).Error; err != nil {
			return err
		}
		result.TotalUsedBytes, result.UsedBytesToday = usage.TotalUsedBytes, usage.UsedBytesToday
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining, err := readRemainingQuota(ctx, tx, q.UserID, now)
		if err != nil {
			return err
		}
		result.RemainingBytes = remaining
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.UsageSummarySnapshot{}, err
	}
	return result, nil
}
