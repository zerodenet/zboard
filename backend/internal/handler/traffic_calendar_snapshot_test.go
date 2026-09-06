package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTrafficProductionTrendRouteReusesFactsAndSeparatesTimezoneChanges(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	config := model.SystemConfig{ConfigKey: systemTimezoneConfigKey, Value: "UTC"}
	if err := f.h.db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	extra := model.TrafficRecord{UserID: 1, NodeID: 1, ReportID: "previous-utc-day", Nonce: "previous-utc-day", At: time.Date(2026, 8, 31, 20, 30, 0, 0, time.UTC), UsedBytes: 77}
	if err := f.h.db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/traffic/trends?from=2026-09-01&to=2026-09-01"
	var first, repeated, shanghai trafficTrendResponse
	f.get(t, path, true, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, &first)
	f.h.trafficTrendsCache.mu.Lock()
	for _, entry := range f.h.trafficTrendsCache.entries {
		entry.expires = time.Now().Add(time.Minute)
	}
	f.h.trafficTrendsCache.mu.Unlock()
	f.capture()
	f.get(t, path, true, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, &repeated)
	if first.RecordCount != 7 || repeated.RecordCount != 7 || !first.AsOf.Equal(repeated.AsOf) {
		t.Fatalf("UTC first=%+v repeated=%+v", first, repeated)
	}
	for _, sql := range f.log.queries {
		if strings.Contains(sql, "SUM(upload_bytes)") {
			t.Fatalf("production route bypassed cache: %s", sql)
		}
	}
	if err := f.h.db.Model(&config).Update("value", "Asia/Shanghai").Error; err != nil {
		t.Fatal(err)
	}
	f.get(t, path, true, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, &shanghai)
	if shanghai.RecordCount != 8 || len(shanghai.Points) != 1 || shanghai.Points[0].UsedBytes != 1286 || !shanghai.AsOf.After(first.AsOf) {
		t.Fatalf("timezone change reused UTC result: %+v", shanghai)
	}
}
