package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTrafficSnapshotsReuseStatisticsButKeepPagesLive(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	path := "/api/v1/traffic/records?paged=true&bucket=hour&from=2026-09-01&to=2026-09-01"
	var before trafficUsageStatistics
	f.get(t, path+"&view=usage_summary", false, f.h.TrafficUsageRecordsHandler, &before)
	// Keep the test independent of CI scheduling; expiry itself is tested with
	// virtual time in TestTrafficSnapshotExpiryAndErrors.
	f.h.trafficStatisticsCache.mu.Lock()
	for _, entry := range f.h.trafficStatisticsCache.entries {
		entry.expires = time.Now().Add(time.Minute)
	}
	f.h.trafficStatisticsCache.mu.Unlock()
	record := model.TrafficRecord{UserID: 1, NodeID: 1, SubscriptionID: 1, ReportID: "new", Nonce: "new", At: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), UsedBytes: 15, RawBytes: 15, ProtocolMultiplierMilli: 1000}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	f.capture()
	var page struct {
		usageTestPage
		StatisticsAsOf time.Time `json:"statistics_as_of"`
	}
	f.get(t, path, false, f.h.TrafficUsageRecordsHandler, &page)
	if page.Aggregates != before.Aggregates || !page.StatisticsAsOf.Equal(before.AsOf) || len(page.Items) != 5 || page.Items[0].UsedBytes != 15 {
		t.Fatalf("snapshot/live page=%+v before=%+v", page, before)
	}
	if len(f.log.queries) != 3 || len(f.log.authContexts) != 1 {
		t.Fatalf("cached request queries=%d auth=%d", len(f.log.queries), len(f.log.authContexts))
	}
	f.h.trafficStatisticsCache.mu.Lock()
	for _, entry := range f.h.trafficStatisticsCache.entries {
		entry.expires = time.Time{}
	}
	f.h.trafficStatisticsCache.mu.Unlock()
	var refreshed trafficUsageStatistics
	f.get(t, path+"&view=usage_summary", false, f.h.TrafficUsageRecordsHandler, &refreshed)
	if refreshed.Total != 5 || refreshed.Aggregates.UsedBytes != 225 || !refreshed.AsOf.After(before.AsOf) {
		t.Fatalf("refreshed=%+v", refreshed)
	}
}

func TestTrafficSnapshotKeysPreserveScopeFiltersAndBucket(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	closeView, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	defer closeView()
	base := "/api/v1/admin/traffic/records?view=usage_summary&from=2026-09-01&to=2026-09-01"
	for _, item := range []struct {
		query string
		bytes int64
		total int64
	}{
		{"&bucket=hour", 1209, 5},
		{"&bucket=hour&user_id=1", 210, 4},
		{"&bucket=hour&user_id=2", 999, 1},
		{"&bucket=hour&user_id=1&node_id=1", 120, 3},
		{"&bucket=hour&user_id=1&protocol_endpoint_id=1", 10, 1},
		{"&bucket=hour&user_id=1&subscription_id=999", 0, 0},
		{"&bucket=day&user_id=1", 210, 2},
	} {
		var result trafficUsageStatistics
		f.get(t, base+item.query, true, f.h.TrafficUsageRecordsHandler, &result)
		if result.Aggregates.UsedBytes != item.bytes || result.Total != item.total {
			t.Fatalf("query=%s result=%+v", item.query, result)
		}
	}
	var otherWindow trafficUsageStatistics
	f.get(t, "/api/v1/admin/traffic/records?view=usage_summary&from=2026-09-02&to=2026-09-02&bucket=hour", true, f.h.TrafficUsageRecordsHandler, &otherWindow)
	if otherWindow.Total != 0 {
		t.Fatalf("date range collided: %+v", otherWindow)
	}
}

func TestTrafficTrendsReuseFactsButReadAuthorizationAndFacetsLive(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	sub := model.Subscription{UserID: 1, Status: subStatusActive}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/traffic/trends?from=2026-09-01&to=2026-09-01"
	var first, second trafficTrendResponse
	f.get(t, path, false, f.h.TrafficTrendsHandler, &first)
	f.h.trafficTrendsCache.mu.Lock()
	for _, entry := range f.h.trafficTrendsCache.entries {
		entry.expires = time.Now().Add(time.Minute)
	}
	f.h.trafficTrendsCache.mu.Unlock()
	if err := f.h.db.Model(&sub).Update("status", subStatusExpired).Error; err != nil {
		t.Fatal(err)
	}
	f.capture()
	f.get(t, path+"&include_subscriptions=true", false, f.h.TrafficTrendsHandler, &second)
	if first.RecordCount != 6 || second.RecordCount != first.RecordCount || !second.AsOf.Equal(first.AsOf) || len(second.Subscriptions) != 1 || second.Subscriptions[0].Status != subStatusExpired {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if len(f.log.queries) != 1 || len(f.log.authContexts) != 1 {
		t.Fatalf("trend cache queries=%d auth=%d", len(f.log.queries), len(f.log.authContexts))
	}
	for _, handler := range []http.HandlerFunc{f.h.TrafficTrendsHandler, f.h.TrafficUsageRecordsHandler} {
		// Warm the statistics cache too, before revoking the account.
		if err := f.h.db.Model(&model.User{}).Where("id = 1").Update("status", userStatusActive).Error; err != nil {
			t.Fatal(err)
		}
		requestPath := path + "&view=usage_summary"
		var ignored any
		f.get(t, requestPath, false, handler, &ignored)
		if err := f.h.db.Model(&model.User{}).Where("id = 1").Update("status", 0).Error; err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler(response, announcementRequest(http.MethodGet, requestPath, f.token, ""))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("revoked account received cached data: %d %s", response.Code, response.Body.String())
		}
	}
}
