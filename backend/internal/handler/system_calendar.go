package handler

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const systemTimezoneConfigKey = "system_timezone"

type systemCalendarBucket struct {
	Key      string
	StartUTC time.Time
	EndUTC   time.Time
}

func normalizeSystemLocation(location *time.Location) *time.Location {
	if location == nil {
		return time.UTC
	}
	return location
}

func (h *handlers) systemTimezoneLocation() *time.Location {
	var config model.SystemConfig
	if err := h.db.Select("value").Where("config_key = ?", systemTimezoneConfigKey).First(&config).Error; err != nil {
		return time.UTC
	}
	location, err := time.LoadLocation(strings.TrimSpace(config.Value))
	if err != nil {
		return time.UTC
	}
	return location
}

func systemDateAt(value time.Time, location *time.Location) time.Time {
	location = normalizeSystemLocation(location)
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func parseSystemDate(raw string, location *time.Location) (time.Time, error) {
	location = normalizeSystemLocation(location)
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), location)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func inclusiveSystemDayCount(from, to time.Time, maxDays int) (int, error) {
	if to.Before(from) {
		return 0, fmt.Errorf("to must not be earlier than from")
	}
	count := 0
	for cursor := from; !cursor.After(to); cursor = cursor.AddDate(0, 0, 1) {
		count++
		if count > maxDays {
			return 0, fmt.Errorf("traffic trend range cannot exceed %d days", maxDays)
		}
	}
	return count, nil
}

func parseTrafficTrendRangeInLocation(values url.Values, now time.Time, location *time.Location) (time.Time, time.Time, int, error) {
	location = normalizeSystemLocation(location)
	today := systemDateAt(now, location)
	from := today.AddDate(0, 0, -6)
	to := today
	var err error
	if raw := strings.TrimSpace(values.Get("from")); raw != "" {
		from, err = parseSystemDate(raw, location)
		if err != nil {
			return time.Time{}, time.Time{}, 0, fmt.Errorf("from must use YYYY-MM-DD")
		}
	}
	if raw := strings.TrimSpace(values.Get("to")); raw != "" {
		to, err = parseSystemDate(raw, location)
		if err != nil {
			return time.Time{}, time.Time{}, 0, fmt.Errorf("to must use YYYY-MM-DD")
		}
	}
	days, err := inclusiveSystemDayCount(from, to, trafficTrendMaxDays)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	return from, to, days, nil
}

func systemDayBuckets(from time.Time, days int, location *time.Location) []systemCalendarBucket {
	location = normalizeSystemLocation(location)
	cursor := systemDateAt(from, location)
	buckets := make([]systemCalendarBucket, 0, days)
	for index := 0; index < days; index++ {
		next := cursor.AddDate(0, 0, 1)
		buckets = append(buckets, systemCalendarBucket{
			Key:      cursor.Format("2006-01-02"),
			StartUTC: cursor.UTC(),
			EndUTC:   next.UTC(),
		})
		cursor = next
	}
	return buckets
}

func systemBucketCaseExpression(column string, buckets []systemCalendarBucket) (string, []interface{}) {
	parts := make([]string, 0, len(buckets))
	args := make([]interface{}, 0, len(buckets)*2)
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("WHEN %s >= ? AND %s < ? THEN '%s'", column, column, bucket.Key))
		args = append(args, bucket.StartUTC, bucket.EndUTC)
	}
	if len(parts) == 0 {
		return "NULL", nil
	}
	return "CASE " + strings.Join(parts, " ") + " END", args
}

func (h *handlers) loadTrafficTrendRowsInLocation(query *gorm.DB, from time.Time, days int, location *time.Location) ([]trafficTrendAggregateRow, error) {
	buckets := systemDayBuckets(from, days, location)
	expression, args := systemBucketCaseExpression("record_at", buckets)
	rows := make([]trafficTrendAggregateRow, 0, len(buckets))
	err := query.
		Select(expression+" AS day, COALESCE(SUM(upload_bytes), 0) AS upload_bytes, COALESCE(SUM(download_bytes), 0) AS download_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COUNT(*) AS record_count", args...).
		Group("day").
		Order("day ASC").
		Scan(&rows).Error
	return rows, err
}

func resolveDashboardPeriodInLocation(raw string, now time.Time, location *time.Location) (dashboardPeriod, error) {
	location = normalizeSystemLocation(location)
	nowUTC := now.UTC()
	localNow := now.In(location)
	startOfToday := systemDateAt(now, location)
	period := dashboardPeriod{
		Range:    strings.ToLower(strings.TrimSpace(raw)),
		To:       nowUTC,
		Bucket:   "day",
		Timezone: location.String(),
	}
	if period.Range == "" {
		period.Range = dashboardRange7Days
	}
	comparisonShiftDays := 0
	switch period.Range {
	case dashboardRangeToday:
		period.From = startOfToday.UTC()
		period.Bucket = "hour"
		comparisonShiftDays = 1
	case dashboardRange7Days:
		period.From = startOfToday.AddDate(0, 0, -6).UTC()
		comparisonShiftDays = 7
	case dashboardRange30Days:
		period.From = startOfToday.AddDate(0, 0, -29).UTC()
		comparisonShiftDays = 30
	default:
		return dashboardPeriod{}, fmt.Errorf("range must be one of today, 7d, or 30d")
	}
	period.PreviousFrom = period.From.In(location).AddDate(0, 0, -comparisonShiftDays).UTC()
	period.PreviousTo = localNow.AddDate(0, 0, -comparisonShiftDays).UTC()
	return period, nil
}

func dashboardCalendarBuckets(period dashboardPeriod, location *time.Location) []systemCalendarBucket {
	location = normalizeSystemLocation(location)
	cursor := period.From.In(location)
	if period.Bucket == "hour" {
		cursor = time.Date(cursor.Year(), cursor.Month(), cursor.Day(), cursor.Hour(), 0, 0, 0, location)
	} else {
		cursor = systemDateAt(cursor, location)
	}
	buckets := make([]systemCalendarBucket, 0, 32)
	for index := 0; cursor.UTC().Before(period.To); index++ {
		next := cursor.Add(time.Hour)
		if period.Bucket != "hour" {
			next = cursor.AddDate(0, 0, 1)
		}
		buckets = append(buckets, systemCalendarBucket{
			Key:      fmt.Sprintf("b%03d", index),
			StartUTC: cursor.UTC(),
			EndUTC:   next.UTC(),
		})
		cursor = next
	}
	return buckets
}

func (h *handlers) loadDashboardTrendInLocation(period dashboardPeriod, location *time.Location) ([]dashboardTrendPoint, error) {
	buckets := dashboardCalendarBuckets(period, location)
	if len(buckets) == 0 {
		return []dashboardTrendPoint{}, nil
	}
	expression, args := systemBucketCaseExpression("paid_at", buckets)
	rows := make([]dashboardTrendRow, 0, len(buckets))
	query := h.db.Model(&model.Order{}).
		Select(expression+` AS bucket_start,
			COALESCE(SUM((CASE WHEN paid_amount > 0 THEN paid_amount ELSE amount_cents END) - refund_amount), 0) AS revenue_cents,
			COUNT(*) AS paid_orders,
			COALESCE(SUM(CASE WHEN order_type = 'new' THEN 1 ELSE 0 END), 0) AS new_orders,
			COALESCE(SUM(CASE WHEN order_type = 'renew' THEN 1 ELSE 0 END), 0) AS renew_orders`, args...).
		Where("status = ? AND paid_at IS NOT NULL AND paid_at >= ? AND paid_at < ?", orderStatusPaid, period.From, period.To).
		Group("bucket_start").Order("bucket_start ASC")
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	byBucket := make(map[string]dashboardTrendRow, len(rows))
	for _, row := range rows {
		byBucket[strings.TrimSpace(row.BucketStart)] = row
	}
	points := make([]dashboardTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		row := byBucket[bucket.Key]
		points = append(points, dashboardTrendPoint{
			BucketStart:  bucket.StartUTC,
			RevenueCents: row.RevenueCents,
			PaidOrders:   row.PaidOrders,
			NewOrders:    row.NewOrders,
			RenewOrders:  row.RenewOrders,
		})
	}
	return points, nil
}
