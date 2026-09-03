package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type usageTestPage struct {
	Items      []trafficUsageBucket
	Total      int64
	Aggregates trafficRecordAggregates
	Facets     trafficPageReferences
	Page       struct {
		NextCursor     *string `json:"next_cursor"`
		PreviousCursor *string `json:"previous_cursor"`
	}
}

func (f trafficReadFixture) seedUsage(t *testing.T) {
	t.Helper()
	// Non-monotonic timestamps and interleaved IDs expose pre-group ID filters.
	for _, row := range []struct {
		id         uint
		stamp      string
		node, user uint
		bytes      int64
	}{
		{1, "01:05:00", 1, 1, 10}, {2, "02:01:00", 1, 1, 20},
		{3, "01:10:00", 2, 1, 30}, {4, "01:55:00", 1, 1, 40},
		{5, "00:01:00", 1, 1, 50}, {6, "01:59:59", 2, 1, 60},
		{7, "02:01:00", 1, 2, 999},
	} {
		at, _ := time.Parse("2006-01-02 15:04:05", "2026-09-01 "+row.stamp)
		record := model.TrafficRecord{ID: row.id, NodeID: row.node, UserID: row.user, SubscriptionID: 1, ProtocolEndpointID: row.id, ReportID: fmt.Sprint(row.id), Nonce: fmt.Sprint(row.id), At: at, ProtocolMultiplierMilli: 1000, RawBytes: row.bytes, UsedBytes: row.bytes, DownloadBytes: row.bytes}
		if err := f.h.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrafficUsageCursorPreservesCompleteBucketAndScope(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	path := "/api/v1/traffic/records?paged=true&bucket=hour&from=2026-09-01&to=2026-09-01&limit=1"
	var first, second, third, back usageTestPage
	f.get(t, path, false, f.h.TrafficUsageRecordsHandler, &first)
	if first.Total != 4 || first.Aggregates.UsedBytes != 210 || first.Items[0].ID != 2 || first.Page.NextCursor == nil {
		t.Fatalf("first = %+v", first)
	}
	f.capture()
	f.get(t, path+"&cursor="+url.QueryEscape(*first.Page.NextCursor), false, f.h.TrafficUsageRecordsHandler, &second)
	if len(second.Items) != 1 || second.Items[0].ID != 3 || second.Items[0].UsedBytes != 90 || second.Items[0].RecordCount != 2 {
		t.Fatalf("second = %+v", second)
	}
	if second.Total != first.Total || second.Aggregates != first.Aggregates {
		t.Fatal("cursor changed range-wide totals")
	}
	if len(f.log.queries) != 5 || strings.Contains(f.log.queries[1], "SUM(") || !strings.Contains(f.log.queries[2], "record_at < '2026-09-01 03:00:00'") {
		t.Fatalf("count/seek queries = %v", f.log.queries)
	}
	assertTrafficScopeIndexPlan(t, f, f.log.queries[2])
	f.get(t, path+"&cursor="+url.QueryEscape(*second.Page.NextCursor), false, f.h.TrafficUsageRecordsHandler, &third)
	if third.Items[0].ID != 1 || third.Items[0].UsedBytes != 50 || third.Items[0].RecordCount != 2 {
		t.Fatalf("split cursor bucket = %+v", third)
	}
	f.get(t, path+"&cursor="+url.QueryEscape(*third.Page.PreviousCursor), false, f.h.TrafficUsageRecordsHandler, &back)
	if !reflect.DeepEqual(back.Items, second.Items) {
		t.Fatalf("back = %+v, second = %+v", back.Items, second.Items)
	}
}

func TestTrafficSummaryCancellationStopsBeforeFollowingAggregates(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.capture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.log.afterAuth = cancel
	r := httptest.NewRecorder()
	f.h.TrafficSummaryHandler(r, announcementRequest(http.MethodGet, "/api/v1/traffic/summary", f.token, "").WithContext(ctx))
	if r.Code != http.StatusInternalServerError || len(f.log.queries) != 1 || f.log.contexts[0] != ctx {
		t.Fatalf("status=%d query count=%d", r.Code, len(f.log.queries))
	}
}

func TestTrafficUsagePrecisionOffsetAndNullSubscriptionGrouping(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	for bucket, count := range map[string]int64{"minute": 6, "hour": 4, "day": 2} {
		var page usageTestPage
		f.get(t, "/api/v1/traffic/records?paged=true&bucket="+bucket+"&from=2026-09-01&to=2026-09-01&limit=1&offset=1", false, f.h.TrafficUsageRecordsHandler, &page)
		if page.Total != count || len(page.Items) != 1 || page.Items[0].RecordAt.IsZero() {
			t.Fatalf("%s = %+v", bucket, page)
		}
	}
	// Legacy null and explicit zero are the same human-facing subscription.
	if err := f.h.db.Model(&model.TrafficRecord{}).Where("id = 1").Update("subscription_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.TrafficRecord{}).Where("id = 4").Update("subscription_id", 0).Error; err != nil {
		t.Fatal(err)
	}
	var page usageTestPage
	f.get(t, "/api/v1/traffic/records?paged=true&bucket=hour&from=2026-09-01&to=2026-09-01", false, f.h.TrafficUsageRecordsHandler, &page)
	if page.Total != 4 || page.Items[2].SubscriptionID != 0 || page.Items[2].UsedBytes != 50 {
		t.Fatalf("null grouping = %+v", page)
	}
}

func TestTrafficUsageAndNodeSeriesQueriesHonorRequestCancellation(t *testing.T) {
	for _, view := range []string{"", "&view=node_series", "&view=raw"} {
		t.Run(view, func(t *testing.T) {
			f := newTrafficReadFixture(t)
			f.capture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.log.afterAuth = cancel
			r := httptest.NewRecorder()
			f.h.TrafficUsageRecordsHandler(r, announcementRequest(http.MethodGet, "/api/v1/traffic/records?paged=true&bucket=hour"+view, f.token, "").WithContext(ctx))
			wantQueries := 1
			if view == "" {
				wantQueries = 0
			} // Canceled statistics transaction cannot begin.
			if r.Code != http.StatusInternalServerError || len(f.log.queries) != wantQueries || (wantQueries > 0 && f.log.contexts[0] != ctx) {
				t.Fatalf("status=%d query count=%d", r.Code, len(f.log.queries))
			}
		})
	}
}

func TestTrafficNodeSeriesRunsOnSQLiteWithSameByteFacts(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	var series trafficNodeSeriesResponse
	f.get(t, "/api/v1/traffic/records?view=node_series&bucket=hour&from=2026-09-01&to=2026-09-01", false, f.h.TrafficUsageRecordsHandler, &series)
	var total int64
	for _, point := range series.Points {
		total += point.UsedBytes
		if point.RecordAt.IsZero() {
			t.Fatal("missing timestamp")
		}
	}
	if total != 210 || len(series.Points) != 4 || len(series.Nodes) != 2 {
		t.Fatalf("series=%+v", series)
	}
}

func TestTrafficUsagePageReferencesAreBoundedIndependentAndAccountScoped(t *testing.T) {
	f := newTrafficReadFixture(t)
	group := model.NodeGroup{Name: "Traffic reference group", Code: "traffic-ref", IsEnabled: true}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{ID: 1, Name: "Owned plan", Slug: "owned", NodeGroupID: group.ID}
	if err := f.h.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	foreignPlan := model.Plan{ID: 2, Name: "Other account private plan", Slug: "foreign", NodeGroupID: group.ID}
	if err := f.h.db.Create(&foreignPlan).Error; err != nil {
		t.Fatal(err)
	}
	for _, sub := range []model.Subscription{{ID: 1, UserID: 1, PlanID: 1}, {ID: 2, UserID: 2, PlanID: 2}} {
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for id := uint(1); id <= 10; id++ {
		node := model.Node{ID: id, Name: fmt.Sprintf("Historical node %d", id), Address: "private.invalid", SSHPwd: "fixture-only-secret"}
		if err := f.h.db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
		sub := uint(1)
		if id == 9 {
			sub = 2
		} // Invalid raw association cannot disclose foreign labels.
		if id == 10 {
			sub = 999
		} // Deleted subscription uses an explicit missing ref.
		record := model.TrafficRecord{ID: id, UserID: 1, SubscriptionID: sub, NodeID: id, ReportID: fmt.Sprint(id), Nonce: fmt.Sprint(id), At: at, UsedBytes: int64(100 - id), ProtocolMultiplierMilli: 1000}
		if err := f.h.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	var series trafficNodeSeriesResponse
	f.get(t, "/api/v1/traffic/records?view=node_series&bucket=hour&from=2026-09-01&to=2026-09-01", false, f.h.TrafficUsageRecordsHandler, &series)
	if len(series.Nodes) != 8 || !series.Truncated {
		t.Fatalf("ranking=%+v", series.Nodes)
	}
	f.capture()
	var page usageTestPage
	f.get(t, "/api/v1/traffic/records?paged=true&bucket=hour&from=2026-09-01&to=2026-09-01&limit=3", false, f.h.TrafficUsageRecordsHandler, &page)
	if len(page.Facets.Nodes) != 3 || page.Facets.Nodes["9"].DisplayName != "Historical node 9" || page.Facets.Nodes["10"].DisplayName != "Historical node 10" {
		t.Fatalf("page nodes=%+v", page.Facets.Nodes)
	}
	if !page.Facets.Subscriptions["2"].Missing || !page.Facets.Subscriptions["999"].Missing || page.Facets.Subscriptions["1"].DisplayName != "Owned plan" {
		t.Fatalf("page subscriptions=%+v", page.Facets.Subscriptions)
	}
	if len(f.log.queries) != 5 || !strings.Contains(f.log.queries[4], "SELECT id, name, region, lifecycle_status") {
		t.Fatalf("reference reads must be two bounded, narrow batches: %v", f.log.queries)
	}
}
