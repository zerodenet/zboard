package handler

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// All projections are loaded inside the caller's read transaction. Comparing a
// subscription from before a concurrent settlement with its ledger from after
// that settlement would manufacture a reconciliation error.
func loadTrafficReconciliation(db *gorm.DB, userID, subscriptionID uint, now time.Time, paged, issuesOnly bool, offset, limit int) (interface{}, error) {
	baseQuery := func() *gorm.DB {
		query := db.Model(&model.Subscription{})
		if userID != 0 {
			query = query.Where("subscriptions.user_id = ?", userID)
		}
		if subscriptionID != 0 {
			query = query.Where("subscriptions.id = ?", subscriptionID)
		}
		return query
	}
	trafficTotalsQuery := func() *gorm.DB {
		return trafficReconciliationTotalsQuery(db, baseQuery(), userID != 0 || subscriptionID != 0)
	}
	var total int64
	aggregates := trafficReconciliationAggregates{}
	query := baseQuery().Order("subscriptions.id desc")
	if paged {
		if err := baseQuery().
			Joins("LEFT JOIN (?) AS reconciliation_totals ON reconciliation_totals.subscription_id = subscriptions.id", trafficTotalsQuery()).
			Select(`
				COUNT(*) AS subscription_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used = COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS matched_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used > COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS missing_records_count,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used < COALESCE(reconciliation_totals.recorded_bytes, 0) THEN 1 ELSE 0 END), 0) AS over_recorded_count,
				COALESCE(SUM(subscriptions.flow_used), 0) AS flow_used,
				COALESCE(SUM(COALESCE(reconciliation_totals.recorded_bytes, 0)), 0) AS recorded_bytes,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used > COALESCE(reconciliation_totals.recorded_bytes, 0) THEN subscriptions.flow_used - COALESCE(reconciliation_totals.recorded_bytes, 0) ELSE 0 END), 0) AS missing_bytes,
				COALESCE(SUM(CASE WHEN subscriptions.flow_used < COALESCE(reconciliation_totals.recorded_bytes, 0) THEN COALESCE(reconciliation_totals.recorded_bytes, 0) - subscriptions.flow_used ELSE 0 END), 0) AS over_recorded_bytes
			`).
			Scan(&aggregates).Error; err != nil {
			return nil, err
		}
		if issuesOnly {
			query = query.
				Joins("LEFT JOIN (?) AS reconciliation_totals ON reconciliation_totals.subscription_id = subscriptions.id", trafficTotalsQuery()).
				Where("subscriptions.flow_used <> COALESCE(reconciliation_totals.recorded_bytes, 0)")
		}
		total = aggregates.SubscriptionCount
		if issuesOnly {
			total = aggregates.MissingRecordsCount + aggregates.OverRecordedCount
		}
		query = query.Offset(offset).Limit(limit)
	}
	// Issue filtering already computes these totals; return them with the page
	// instead of scanning the page's history a third time.
	type reconciliationRow struct {
		model.Subscription `gorm:"embedded"`
		RecordedBytes      int64 `gorm:"column:recorded_bytes"`
	}
	subscriptions := make([]reconciliationRow, 0)
	query = query.Select("subscriptions.*")
	if paged && issuesOnly {
		query = query.Select("subscriptions.*, COALESCE(reconciliation_totals.recorded_bytes, 0) AS recorded_bytes")
	}
	if err := query.Scan(&subscriptions).Error; err != nil {
		return nil, err
	}

	totals := make(map[uint]int64, len(subscriptions))
	if len(subscriptions) > 0 && !(paged && issuesOnly) {
		ids := make([]uint, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			ids = append(ids, subscription.ID)
		}
		var aggregates []struct {
			SubscriptionID uint  `gorm:"column:subscription_id"`
			RecordedBytes  int64 `gorm:"column:recorded_bytes"`
		}
		if err := db.Model(&model.TrafficRecord{}).
			Select("subscription_id, COALESCE(SUM(used_bytes), 0) AS recorded_bytes").
			Where("subscription_id IN ?", ids).
			Group("subscription_id").Scan(&aggregates).Error; err != nil {
			return nil, err
		}
		for _, aggregate := range aggregates {
			totals[aggregate.SubscriptionID] = aggregate.RecordedBytes
		}
	}

	items := make([]trafficReconciliationItem, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		recorded := totals[subscription.ID]
		if paged && issuesOnly {
			recorded = subscription.RecordedBytes
		}
		difference := subscription.FlowUsed - recorded
		items = append(items, trafficReconciliationItem{
			SubscriptionID: subscription.ID,
			UserID:         subscription.UserID,
			PlanID:         subscription.PlanID,
			Status:         effectiveSubscriptionStatus(subscription.Subscription, now),
			FlowUsed:       subscription.FlowUsed,
			RecordedBytes:  recorded,
			Difference:     difference,
			Result:         trafficReconciliationResult(difference),
		})
	}
	if paged {
		data := pagedData(items, total, offset, limit)
		data["aggregates"] = aggregates
		return data, nil
	}
	return items, nil
}
