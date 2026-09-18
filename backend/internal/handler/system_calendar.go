package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
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
	config, err := h.services.Settings.Get(context.Background(), systemTimezoneConfigKey)
	if err != nil {
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
