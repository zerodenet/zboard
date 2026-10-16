package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/readtiming"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func (s UsageSummary) ReadQuota(ctx context.Context, actor uint, q metering.UsageSummaryQuery) (metering.UsageQuotaSnapshot, error) {
	defer readtiming.Start(ctx, "usage_quota")()
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.UsageQuotaSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	now := time.Now().UTC()
	// Current quota is an indexed entitlement aggregate. It must not queue on
	// the reporting pool or wait for history scans; no cached balance is used.
	remaining, err := readRemainingQuota(ctx, s.DB, q.UserID, now)
	if err != nil {
		return metering.UsageQuotaSnapshot{}, err
	}
	return metering.UsageQuotaSnapshot{RemainingBytes: remaining, ScopeUser: q.UserID, AsOf: now.Format(time.RFC3339)}, nil
}

func readRemainingQuota(ctx context.Context, db *gorm.DB, user uint, now time.Time) (int64, error) {
	query := db.WithContext(ctx).Model(&model.Subscription{}).Where("status = ? AND end_at > ? AND flow_used < flow_total", "active", now)
	if user > 0 {
		query = query.Where("user_id = ?", user)
	}
	var remaining int64
	err := query.Select("COALESCE(SUM(flow_total - flow_used), 0)").Scan(&remaining).Error
	return remaining, err
}
