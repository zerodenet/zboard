package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTrafficBreakdownDimensionsScopesTotalsAndPermissionRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	q := metering.BreakdownQuery{Administrative: true, From: from, To: from.Add(24 * time.Hour), Limit: 1}
	service := f.h.services.TrafficBreakdown(trafficBreakdownCacheAdapter{&f.h.trafficBreakdownCache})
	for _, dimension := range []string{"user", "node", "subscription", "protocol_endpoint"} {
		t.Run(dimension, func(t *testing.T) {
			q.Dimension = dimension
			f.capture()
			first, err := service.Read(context.Background(), 99, q)
			if err != nil || len(first.Items) != 1 || first.Total < 1 || first.Totals.UsedBytes != 1209 || first.Totals.RecordCount != 7 {
				t.Fatalf("dimension %s: %+v: %v", dimension, first, err)
			}
			for _, query := range f.log.queries {
				if strings.Contains(query, "traffic_records") {
					t.Fatalf("aligned dimension statistics scanned ledger: %s", query)
				}
			}
			if first.Total > 1 {
				q.Offset = 1
				next, err := service.Read(context.Background(), 99, q)
				if err != nil || next.Totals != first.Totals || next.Total != first.Total || len(next.Items) != 1 || next.Items[0].ID == first.Items[0].ID {
					t.Fatalf("unstable page/totals: %+v: %v", next, err)
				}
				q.Offset = 0
			}
		})
	}
	q.Dimension, q.Administrative, q.UserID = "node", false, 2
	self, err := service.Read(context.Background(), 1, q)
	if err != nil || self.Totals.UsedBytes != 210 || self.Totals.RecordCount != 6 {
		t.Fatalf("self-service broadened ownership: %+v %v", self, err)
	}
	q.Administrative = true
	other, err := service.Read(context.Background(), 99, q)
	if err != nil || other.Totals.UsedBytes != 999 {
		t.Fatalf("administrative filter: %+v %v", other, err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id=99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Read(context.Background(), 99, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatal("cached statistics bypassed current permission:", err)
	}
}

func TestTrafficBreakdownHTTPValidationAndSelfServiceBoundary(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	base := "/api/v1/admin/traffic/records?view=breakdown&from=2026-09-01&to=2026-09-01"
	for _, suffix := range []string{"&dimension=sql", "&limit=201", "&user_id=bad", "&node_id=-1", "&protocol_endpoint_id=0", "&offset=-1"} {
		r := httptest.NewRecorder()
		f.h.TrafficUsageRecordsHandler(r, announcementRequest(http.MethodGet, base+suffix, f.admin, ""))
		if r.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s: %d %s", suffix, r.Code, r.Body.String())
		}
	}
	var result metering.BreakdownSnapshot
	f.get(t, "/api/v1/traffic/records?view=breakdown&dimension=user&user_id=2&from=2026-09-01&to=2026-09-01", false, f.h.TrafficUsageRecordsHandler, &result)
	if result.Total != 1 || result.Items[0].ID != 1 || result.Totals.UsedBytes != 210 {
		t.Fatalf("self-service leaked another user: %+v", result)
	}
	r := httptest.NewRecorder()
	f.h.TrafficUsageRecordsHandler(r, announcementRequest(http.MethodGet, base, f.token, ""))
	if r.Code != http.StatusForbidden {
		t.Fatalf("ordinary account read administrative statistics: %d", r.Code)
	}
}

func TestTrafficBreakdownRetainsMissingAndUnattributedObjectsAndExactEdges(t *testing.T) {
	f := newTrafficReadFixture(t)
	from := time.Date(2026, 9, 1, 1, 15, 0, 0, time.UTC)
	for i, at := range []time.Time{from.Add(-time.Second), from, from.Add(time.Hour), from.Add(2 * time.Hour), from.Add(2*time.Hour + time.Second)} {
		record := model.TrafficRecord{NodeID: uint(i % 2), UserID: 1, ProtocolEndpointID: 987, ReportID: at.String(), Nonce: at.String(), At: at, UsedBytes: 10, UploadBytes: 3, DownloadBytes: 7, RawBytes: 10}
		if err := f.h.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	rebuildFixtureHourly(t, f.h.db)
	q := metering.BreakdownQuery{Administrative: true, Dimension: "node", From: from, To: from.Add(2 * time.Hour), Limit: 50}
	out, err := f.h.services.TrafficBreakdown(nil).Read(context.Background(), 99, q)
	if err != nil || out.Total != 2 || out.Totals.UsedBytes != 20 || out.Totals.UploadBytes != 6 || out.Totals.DownloadBytes != 14 {
		t.Fatalf("exact edges: %+v %v", out, err)
	}
	q.Dimension = "protocol_endpoint"
	out, err = f.h.services.TrafficBreakdown(nil).Read(context.Background(), 99, q)
	if err != nil || out.Total != 1 || !out.Items[0].Missing || out.Items[0].ID != 987 {
		t.Fatalf("missing object lost traffic: %+v %v", out, err)
	}
	q.ProtocolEndpointID = 986
	empty, err := f.h.services.TrafficBreakdown(nil).Read(context.Background(), 99, q)
	if err != nil || empty.Total != 0 || len(empty.Items) != 0 || empty.Totals.UsedBytes != 0 {
		t.Fatalf("empty filter: %+v %v", empty, err)
	}
}

func TestMySQLTrafficBreakdownReadsCommittedAccountingInEveryDimension(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 16)); err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	var subscription model.Subscription
	if err := h.db.First(&subscription, credential.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Now().UTC().Truncate(24 * time.Hour)
	for _, dimension := range []string{"user", "node", "subscription", "protocol_endpoint"} {
		out, err := h.services.TrafficBreakdown(nil).Read(context.Background(), subscription.UserID, metering.BreakdownQuery{Dimension: dimension, From: from, To: from.Add(24 * time.Hour), Limit: 25})
		if err != nil || out.Total != 1 || len(out.Items) != 1 || out.Totals.UsedBytes != 480 || out.Totals.UploadBytes != 160 || out.Totals.DownloadBytes != 320 || out.Totals.RecordCount != 16 || out.Items[0].Missing {
			t.Fatalf("%s: %+v %v", dimension, out, err)
		}
	}
}

func TestTrafficBreakdownDoesNotRevealForeignSubscriptionNames(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.NodeGroup{ID: 88, Name: "foreign-group"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.User{ID: 2, Email: "foreign@example.test", Password: "unused", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.Plan{ID: 88, Name: "foreign-private-plan", Slug: "foreign-private-plan", NodeGroupID: 88}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.Subscription{ID: 2, UserID: 2, PlanID: 88}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	if err := f.h.db.Create(&model.TrafficRecord{UserID: 1, SubscriptionID: 2, NodeID: 1, At: at, UsedBytes: 10}).Error; err != nil {
		t.Fatal(err)
	}
	rebuildFixtureHourly(t, f.h.db)
	q := metering.BreakdownQuery{Dimension: "subscription", From: at.Truncate(24 * time.Hour), To: at.Truncate(24 * time.Hour).Add(24 * time.Hour), Limit: 25}
	self, err := f.h.services.TrafficBreakdown(nil).Read(context.Background(), 1, q)
	if err != nil || self.Total != 1 || !self.Items[0].Missing || self.Items[0].Name != "" || self.Totals.UsedBytes != 10 {
		t.Fatalf("foreign subscription leaked or usage lost: %+v %v", self, err)
	}
	q.Administrative = true
	admin, err := f.h.services.TrafficBreakdown(nil).Read(context.Background(), 99, q)
	if err != nil || admin.Items[0].Name != "foreign-private-plan" {
		t.Fatalf("administrative name resolution: %+v %v", admin, err)
	}
}

func TestTrafficNodeSeriesKeepsProtocolEndpointDrilldown(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	var out trafficNodeSeriesResponse
	f.get(t, "/api/v1/admin/traffic/records?view=node_series&bucket=hour&protocol_endpoint_id=987&from=2026-09-01&to=2026-09-01", true, f.h.TrafficUsageRecordsHandler, &out)
	if len(out.Points) != 0 {
		t.Fatalf("node chart ignored endpoint filter: %+v", out.Points)
	}
}

func TestTrafficDimensionDrilldownDoesNotReuseBroaderConnectionPeaks(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&principalFlowScopeObservation{ScopeType: principalFlowScopeUser, ScopeID: 1, ActiveFlows: 5, NodeID: 1, EventID: "peak", Source: "lifecycle", ObservedAt: time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"node_id=987", "protocol_endpoint_id=987"} {
		var out trafficTrendResponse
		f.get(t, "/api/v1/admin/traffic/trends?user_id=1&from=2026-09-01&to=2026-09-01&"+filter, true, f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler, &out)
		if out.PeakConnections != nil || out.ConnectionSampleCount != 0 {
			t.Fatalf("%s reused account-wide connections: %+v", filter, out)
		}
		for _, point := range out.Points {
			if point.PeakConnections != nil {
				t.Fatalf("%s reused daily account peak: %+v", filter, point)
			}
		}
	}
}
