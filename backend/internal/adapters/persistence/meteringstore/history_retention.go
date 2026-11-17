package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"time"
)

func historyCoverage(from, to time.Time, bucket string, now time.Time) metering.HistoryCoverage {
	c := metering.HistoryCoverage{From: from.UTC(), To: to.UTC(), Bucket: bucket, RetainedFrom: now.UTC().Add(-LedgerDayHorizon).Truncate(24 * time.Hour)}
	if !c.To.After(c.RetainedFrom) {
		c.From, c.To = c.RetainedFrom, c.RetainedFrom
		c.Expired, c.Truncated = true, true
		return c
	}
	if c.From.Before(c.RetainedFrom) {
		c.From = c.RetainedFrom
		c.Truncated = true
	}
	width := time.Minute
	if c.From.Before(now.Add(-LedgerHourHorizon).Truncate(24*time.Hour)) || bucket == "day" {
		width = 24 * time.Hour
		c.Bucket = "day"
	} else if c.From.Before(now.Add(-LedgerMinuteHorizon)) || bucket == "hour" {
		width = time.Hour
		c.Bucket = "hour"
	}
	if c.Bucket != bucket {
		c.Truncated = true
	}
	// Return whole available buckets. Never claim a partial historical bucket is
	// an exact minute result. Clients receive the effective bounds with the data.
	c.From = c.From.Truncate(width)
	if t := c.To.Truncate(width); t.Before(c.To) {
		c.To = t.Add(width)
	}
	if !c.From.Equal(from) || !c.To.Equal(to) {
		c.Truncated = true
	}
	return c
}

// Once the available source precision can no longer represent a timezone's
// partial bucket edges, report UTC calendar days explicitly. Exact recent
// local-day views keep the original calendar (including DST and quarter hours).
func normalizeTrafficTrendHistory(q metering.TrafficTrendQuery, now time.Time) (metering.TrafficTrendQuery, *metering.HistoryCoverage) {
	partial := false
	for _, b := range q.Buckets {
		if !b.StartUTC.Equal(b.StartUTC.Truncate(time.Hour)) || !b.EndUTC.Equal(b.EndUTC.Truncate(time.Hour)) {
			partial = true
			break
		}
	}
	if !q.From.Before(now.Add(-LedgerHourHorizon).Truncate(24*time.Hour)) && !(partial && q.From.Before(now.Add(-LedgerMinuteHorizon))) {
		return q, nil
	}
	from, _ := time.Parse("2006-01-02", q.From.Format("2006-01-02"))
	to := from.AddDate(0, 0, q.Days)
	c := historyCoverage(from, to, "day", now)
	c.Timezone = "UTC"
	c.Truncated = true
	q.From, q.Timezone = c.From, "UTC"
	q.Buckets = nil
	q.Days = 0
	for day := c.From; day.Before(c.To); day = day.AddDate(0, 0, 1) {
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day, EndUTC: day.AddDate(0, 0, 1)})
		q.Days++
	}
	return q, &c
}
