package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"testing"
	"time"
)

func TestCalendarHistoryReportsPrecisionChangeWithoutClaimingExactEdges(t *testing.T) {
	zone, e := time.LoadLocation("Asia/Kathmandu")
	if e != nil {
		t.Fatal(e)
	}
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, zone)
	q := metering.TrafficTrendQuery{From: from, Days: 2, Timezone: zone.String()}
	for i := 0; i < q.Days; i++ {
		day := from.AddDate(0, 0, i)
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()})
	}
	recent, coverage := normalizeTrafficTrendHistory(q, from.Add(12*time.Hour))
	if coverage != nil || recent.Timezone != zone.String() || !recent.From.Equal(from) {
		t.Fatal("recent edges lost their exact calendar", recent, coverage)
	}
	historical, coverage := normalizeTrafficTrendHistory(q, from.AddDate(0, 0, 10))
	if coverage == nil || !coverage.Truncated || coverage.Timezone != "UTC" || historical.Timezone != "UTC" || historical.Days != 2 {
		t.Fatal("coarse history advertised exact local dates", historical, coverage)
	}
	for _, day := range historical.Buckets {
		if !day.StartUTC.Equal(day.StartUTC.Truncate(24*time.Hour)) || !day.EndUTC.Equal(day.EndUTC.Truncate(24*time.Hour)) {
			t.Fatal("history bounds do not match UTC source precision", day)
		}
	}
}
