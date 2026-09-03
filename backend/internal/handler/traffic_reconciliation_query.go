package handler

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func trafficReconciliationTotalsQuery(db, scope *gorm.DB, scoped bool) *gorm.DB {
	ids := scope.Select("subscriptions.id")
	if scoped && db.Dialector.Name() == "mysql" {
		// MySQL can reorder an IN semijoin to scan every traffic row first,
		// even when almost all traffic belongs to another account. Drive the
		// scoped aggregate from subscriptions and indexed subscription lookups.
		// Do not filter traffic.user_id: wrong-owner records are audit facts.
		return db.Table("(?) AS reconciliation_scope", ids).
			Joins("STRAIGHT_JOIN traffic_records ON traffic_records.subscription_id = reconciliation_scope.id").
			Select("traffic_records.subscription_id, COALESCE(SUM(traffic_records.used_bytes), 0) AS recorded_bytes").
			Group("traffic_records.subscription_id")
	}
	// Whole-site reconciliation legitimately reads the whole ledger. Leave
	// its join order to the optimizer, and keep SQLite's indexed IN plan.
	return db.Model(&model.TrafficRecord{}).
		Where("subscription_id IN (?)", ids).
		Select("subscription_id, COALESCE(SUM(used_bytes), 0) AS recorded_bytes").
		Group("subscription_id")
}
