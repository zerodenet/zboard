package meteringstore

import "gorm.io/gorm"

func ReconciliationTotalsQuery(db, scope *gorm.DB, scoped bool) *gorm.DB {
	ids := scope.Select("subscriptions.id")
	if scoped && db.Dialector.Name() == "mysql" {
		result := db.Table("(?) AS reconciliation_scope", ids).Joins("STRAIGHT_JOIN metering_settlement_results ON metering_settlement_results.subscription_id = reconciliation_scope.id").Select("metering_settlement_results.subscription_id,metering_settlement_results.used_bytes")
		legacy := db.Table("(?) AS reconciliation_scope", ids).Joins("STRAIGHT_JOIN traffic_records ON traffic_records.subscription_id = reconciliation_scope.id").Where("traffic_records.result_recorded = 0").Select("traffic_records.subscription_id,traffic_records.used_bytes")
		return db.Table("(? UNION ALL ?) AS reconciliation_facts", result, legacy).Select("subscription_id,COALESCE(SUM(used_bytes),0) AS recorded_bytes").Group("subscription_id")
	}
	return settlementResultSource(db).Where("subscription_id IN (?)", ids).Select("subscription_id,COALESCE(SUM(used_bytes),0) AS recorded_bytes").Group("subscription_id")
}
