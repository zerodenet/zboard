package observabilitystore

import (
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"gorm.io/gorm"
)

func loadDashboardTrend(db *gorm.DB, period observability.DashboardPeriod, buckets []observability.DashboardTrendBucket) ([]observability.DashboardTrendPoint, error) {
	trend, _, _, err := loadDashboardTrendFacts(db, period, buckets)
	return trend, err
}
func loadDashboardTrendFacts(db *gorm.DB, period observability.DashboardPeriod, buckets []observability.DashboardTrendBucket) ([]observability.DashboardTrendPoint, string, bool, error) {
	if len(buckets) == 0 {
		return []observability.DashboardTrendPoint{}, "CNY", false, nil
	}
	parts := make([]string, 0, len(buckets))
	args := make([]interface{}, 0, len(buckets)*2)
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("WHEN paid_at >= ? AND paid_at < ? THEN '%s'", bucket.Key))
		args = append(args, bucket.StartUTC, bucket.EndUTC)
	}
	expression := "CASE " + strings.Join(parts, " ") + " END"
	var rows []dashboardTrendRow
	if err := db.Table("orders").Select(expression+` AS bucket_start,
 COALESCE(SUM((CASE WHEN assigned_by > 0 OR paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount),0) AS revenue_cents,
 COUNT(*) AS paid_orders,
 COALESCE(SUM(CASE WHEN order_type = 'new' THEN 1 ELSE 0 END),0) AS new_orders,
 COALESCE(SUM(CASE WHEN order_type IN ('renew','renewal') THEN 1 ELSE 0 END),0) AS renew_orders,
 COUNT(DISTINCT CASE WHEN TRIM(currency) <> '' THEN UPPER(TRIM(currency)) END) AS currency_count,
 COALESCE(MIN(CASE WHEN TRIM(currency) <> '' THEN UPPER(TRIM(currency)) END),'') AS min_currency,
 COALESCE(MAX(CASE WHEN TRIM(currency) <> '' THEN UPPER(TRIM(currency)) END),'') AS max_currency`, args...).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", "paid", period.From, period.To).
		Group("bucket_start").Order("bucket_start ASC").Scan(&rows).Error; err != nil {
		return nil, "", false, err
	}
	byBucket := make(map[string]dashboardTrendRow, len(rows))
	currency, mixed := "", false
	for _, row := range rows {
		if row.CurrencyCount > 1 || row.MinCurrency != row.MaxCurrency {
			mixed = true
		}
		if row.MinCurrency != "" {
			if currency != "" && currency != row.MinCurrency {
				mixed = true
			}
			currency = row.MinCurrency
		}
		byBucket[strings.TrimSpace(row.BucketStart)] = row
	}
	points := make([]observability.DashboardTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		row := byBucket[bucket.Key]
		points = append(points, observability.DashboardTrendPoint{BucketStart: bucket.StartUTC, RevenueCents: row.RevenueCents, PaidOrders: row.PaidOrders, NewOrders: row.NewOrders, RenewOrders: row.RenewOrders})
	}
	if currency == "" {
		currency = "CNY"
	}
	if mixed {
		currency = ""
	}
	return points, currency, mixed, nil
}
