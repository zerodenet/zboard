package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrafficUsageStatisticsSeparateRangeTotalsFromLiveCursorPages(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	path := "/api/v1/admin/traffic/records?paged=true&bucket=hour&from=2026-09-01&to=2026-09-01&user_id=1&limit=1"
	var legacy usageTestPage
	f.get(t, path, true, f.h.TrafficUsageRecordsHandler, &legacy)
	f.capture()
	var summary trafficUsageStatistics
	f.get(t, path+"&view=usage_summary", true, f.h.TrafficUsageRecordsHandler, &summary)
	if summary.Total != legacy.Total || summary.Aggregates != legacy.Aggregates || summary.AsOf.IsZero() {
		t.Fatalf("summary=%+v legacy=%+v", summary, legacy)
	}
	if len(f.log.queries) != 2 {
		t.Fatalf("summary queries=%d", len(f.log.queries))
	}
	for _, suffix := range []string{"", "&cursor=" + *legacy.Page.NextCursor, "&offset=1"} {
		f.capture()
		var page struct {
			Items      []trafficUsageBucket
			Total      *int64
			Aggregates *trafficRecordAggregates
			Page       struct {
				Total      *int64
				NextCursor *string `json:"next_cursor"`
			}
		}
		f.get(t, path+suffix+"&include_totals=false", true, f.h.TrafficUsageRecordsHandler, &page)
		if len(page.Items) != 1 || page.Total != nil || page.Page.Total != nil || page.Aggregates != nil {
			t.Fatalf("omitted totals must be unknown: %+v", page)
		}
		if len(f.log.queries) != 1 || strings.Contains(f.log.queries[0], "COUNT(DISTINCT") {
			t.Fatalf("live page reran range statistics: %v", f.log.queries)
		}
	}
}

func TestTrafficUsageStatisticsSelfScopeAndEmptyResults(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	var stats trafficUsageStatistics
	f.get(t, "/api/v1/traffic/records?view=usage_summary&bucket=hour&from=2026-09-01&to=2026-09-01&user_id=2", false, f.h.TrafficUsageRecordsHandler, &stats)
	if stats.Total != 4 || stats.Aggregates.UsedBytes != 210 {
		t.Fatalf("account stats=%+v", stats)
	}
	f.capture()
	var page usageTestPage
	f.get(t, "/api/v1/traffic/records?paged=true&include_totals=false&bucket=hour&from=2026-09-01&to=2026-09-01&user_id=2", false, f.h.TrafficUsageRecordsHandler, &page)
	if len(page.Items) != 4 || len(f.log.queries) != 3 {
		t.Fatalf("account page should use one bucket and two reference reads: rows=%d queries=%d", len(page.Items), len(f.log.queries))
	}
	for _, row := range page.Items {
		if row.UserID != 1 {
			t.Fatal("live page escaped the account scope")
		}
	}
	f.get(t, "/api/v1/traffic/records?view=usage_summary&bucket=hour&from=2026-09-01&to=2026-09-01&node_id=999", false, f.h.TrafficUsageRecordsHandler, &stats)
	if stats.Total != 0 || stats.Aggregates.UsedBytes != 0 {
		t.Fatalf("empty stats=%+v", stats)
	}
}

func TestTrafficUsageWithoutTotalsStillHonorsCancellationAndRejectsInvalidFlags(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.capture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.log.afterAuth = cancel
	recorder := httptest.NewRecorder()
	f.h.TrafficUsageRecordsHandler(recorder, announcementRequest(http.MethodGet, "/api/v1/traffic/records?paged=true&include_totals=false", f.token, "").WithContext(ctx))
	if recorder.Code != 500 || len(f.log.queries) != 1 || f.log.contexts[0] != ctx {
		t.Fatalf("cancellation status=%d queries=%d", recorder.Code, len(f.log.queries))
	}
	recorder = httptest.NewRecorder()
	f.h.TrafficUsageRecordsHandler(recorder, announcementRequest(http.MethodGet, "/api/v1/traffic/records?paged=true&include_totals=invalid", f.token, ""))
	if recorder.Code != 400 {
		t.Fatal(recorder.Body.String())
	}
}

func TestTrafficUsageOmittedTotalIsExplicitJSONNull(t *testing.T) {
	payload, err := json.Marshal(trafficUsagePageData([]trafficUsageBucket{}, nil, 0, 25, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"total":null`) {
		t.Fatal(string(payload))
	}
}
