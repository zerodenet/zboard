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
		now = tx.NowFunc().UTC()
		result.AsOf = now.Format(time.RFC3339)
		if err := EnsureHourlyUsageReadable(tx, TrafficScope{UserID: q.UserID}, time.Time{}, time.Time{}); err != nil {
			return err
		}
		// A bounded pending overlay keeps current and historical reports exact
		// without waiting for an empty queue or scanning financial history.
		var usage struct{ TotalUsedBytes, UsedBytesToday int64 }
		if err := TrafficAggregateSource(tx, time.Time{}, time.Time{}, TrafficScope{UserID: q.UserID}).Select(
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
