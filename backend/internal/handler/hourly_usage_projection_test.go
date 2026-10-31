package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type hourlyReadTrendCache struct{ calls int }

func (c *hourlyReadTrendCache) Get(_ context.Context, _ [32]byte, load func() (metering.TrafficTrendSnapshot, error)) (metering.TrafficTrendSnapshot, error) {
	c.calls++
	return load()
}

type hourlyReadBreakdownCache struct{ calls int }

func (c *hourlyReadBreakdownCache) Get(_ context.Context, _ [32]byte, load func() (metering.BreakdownSnapshot, error)) (metering.BreakdownSnapshot, error) {
	c.calls++
	return load()
}

// Reporting fixtures insert already-settled historical facts directly. They
// rebuild the derived view explicitly after the 0054 trigger cutover.
func rebuildFixtureHourly(t testing.TB, db *gorm.DB) {
	t.Helper()
	if err := db.Transaction(datastore.RebuildTrafficHourly); err != nil {
		t.Fatal(err)
	}
}

func TestAccountingHourlyProjectionFailurePreservesSettlementAndUnknownReads(t *testing.T) {
	f := newOrderFixture(t)
	h, credential := accountingFixtureFromOrder(t, f)
	ctx := context.Background()
	initial, err := h.services.UsageQuota().Read(ctx, credential.UserID, metering.UsageSummaryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec(`CREATE TRIGGER reject_hourly_chart BEFORE INSERT ON traffic_usage_hourly BEGIN SELECT RAISE(ABORT, 'hourly chart unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 4)
	for i := range events {
		events[i].OccurredAt = time.Now().UTC().Truncate(time.Hour).Add(-time.Minute)
	}
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal("hourly chart failure blocked settlement", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	quota, err := h.services.UsageQuota().Read(ctx, credential.UserID, metering.UsageSummaryQuery{})
	if err != nil || quota.RemainingBytes != initial.RemainingBytes-120 {
		t.Fatal(quota, err)
	}
	if out, err := h.services.UsageSummary().Read(ctx, credential.UserID, metering.UsageSummaryQuery{}); err != nil || out.TotalUsedBytes != 120 {
		t.Fatal("bounded pending summary", out, err)
	}
	day := events[0].OccurredAt.Truncate(24 * time.Hour)
	trendCache := &hourlyReadTrendCache{}
	trendQuery := metering.TrafficTrendQuery{From: day, Days: 1, Timezone: "UTC", Buckets: []metering.TrendBucket{{Key: day.Format("2006-01-02"), StartUTC: day, EndUTC: day.Add(24 * time.Hour)}}}
	if out, err := h.services.TrafficTrends(trendCache).Read(ctx, credential.UserID, trendQuery); err != nil || out.Snapshot.RecordCount != 4 || out.Snapshot.Points[0].UsedBytes != 120 || trendCache.calls != 1 {
		t.Fatal("bounded pending trend", out, trendCache.calls, err)
	}
	breakdownCache := &hourlyReadBreakdownCache{}
	breakdownQuery := metering.BreakdownQuery{Dimension: "node", From: day, To: day.Add(24 * time.Hour), Limit: 25}
	if out, err := h.services.TrafficBreakdown(breakdownCache).Read(ctx, credential.UserID, breakdownQuery); err != nil || out.Totals.UsedBytes != 120 || out.Totals.RecordCount != 4 || breakdownCache.calls != 1 {
		t.Fatal("bounded pending breakdown", out, breakdownCache.calls, err)
	}
	period := observability.DashboardPeriod{Range: "today", From: day, To: day.Add(24 * time.Hour), PreviousFrom: day.Add(-24 * time.Hour), PreviousTo: day}
	if _, err := h.services.Dashboard.View(ctx, period, time.Now().UTC(), nil, "statistics"); err != nil {
		t.Fatal("bounded pending dashboard", err)
	}
	if _, err := h.services.Dashboard.View(ctx, period, time.Now().UTC(), nil, "current"); err != nil {
		t.Fatal("auxiliary pending blocked core dashboard health", err)
	}
	w := httptest.NewRecorder()
	h.TrafficSummaryHandler(w, announcementRequest(http.MethodGet, "/api/v1/traffic/summary", f.token, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("pending chart status=%d body=%s", w.Code, w.Body.String())
	}
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	if count, err := worker.Drain(ctx, 200); err == nil || count != 0 {
		t.Fatal(count, err)
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	if err := h.db.Exec("DROP TRIGGER reject_hourly_chart").Error; err != nil {
		t.Fatal(err)
	}
	// Projection recovery and report replay are independent of quota mutation.
	drainUsageProjections(t, h.db)
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	full, err := h.services.UsageSummary().Read(ctx, credential.UserID, metering.UsageSummaryQuery{})
	if err != nil || full.TotalUsedBytes != 120 || full.RemainingBytes != initial.RemainingBytes-120 {
		t.Fatal(full, err)
	}
	trend, err := h.services.TrafficTrends(trendCache).Read(ctx, credential.UserID, trendQuery)
	if err != nil || trendCache.calls != 2 || trend.Snapshot.RecordCount != 4 || trend.Snapshot.Points[0].UsedBytes != 120 {
		t.Fatal("recovered trend did not load exact facts", trend, trendCache.calls, err)
	}
}

func TestHourlyRebuildAndWorkerPreservePendingAndLegacyConfirmedDeltas(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	ctx := context.Background()
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 4)); err != nil {
		t.Fatal(err)
	}
	// Rebuilding a transfer destination must not pre-apply queued deltas.
	for i := 0; i < 2; i++ {
		rebuildFixtureHourly(t, h.db)
	}
	var used int64
	if err := h.db.Table("traffic_usage_hourly").Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != 0 {
		t.Fatal(used, err)
	}
	drainUsageProjections(t, h.db)
	if err := h.db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 120 {
		t.Fatal(used, err)
	}
	// A pre-0054 pending item has already been synchronously included in the
	// hourly view. Only its daily projection is outstanding after upgrade.
	old := model.TrafficRecord{NodeID: credential.NodeID, UserID: credential.UserID, SubscriptionID: credential.SubscriptionID, ProtocolEndpointID: credential.ProtocolEndpointID, ReportID: "legacy-hourly-confirmed", Nonce: "legacy-hourly-confirmed", At: time.Now().UTC(), RawBytes: 7, UsedBytes: 11}
	if err := h.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	rebuildFixtureHourly(t, h.db)
	if err := h.db.Create(&model.MeteringUsageProjectionPending{TrafficRecordID: old.ID, ProtocolEndpointID: old.ProtocolEndpointID, CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	if err := h.db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 131 {
		t.Fatal("legacy pending hourly counted twice", used, err)
	}
}
