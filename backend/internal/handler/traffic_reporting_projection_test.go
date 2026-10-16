package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTrafficSummaryReadsLiveAccountingProjection(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	checkTrafficSummaryProjection(t, h, credential)
}

func TestMySQLTrafficSummaryReadsLiveAccountingProjection(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	checkTrafficSummaryProjection(t, h, credential)
}

func checkTrafficSummaryProjection(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	var subscription model.Subscription
	if err := h.db.First(&subscription, credential.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	closeRead, err := h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeRead() })
	read := h.services.PrincipalTrends().Repository.(meteringstore.PrincipalTrends).ReadDB
	trace := &trafficQueryLog{Interface: logger.Discard}
	h.services.SetTrafficReadDatabaseForTest(read.Session(&gorm.Session{Logger: trace}))
	assert := func(total, today int64) {
		t.Helper()
		trace.queries = nil
		out, err := h.services.UsageSummary().Read(context.Background(), subscription.UserID, metering.UsageSummaryQuery{})
		if err != nil || out.TotalUsedBytes != total || out.UsedBytesToday != today || out.RemainingBytes != subscription.FlowTotal-total {
			t.Fatalf("live summary: %+v: %v", out, err)
		}
		hourly := 0
		for _, query := range trace.queries {
			if strings.Contains(query, "traffic_records") {
				t.Fatalf("summary scanned ledger: %s", query)
			}
			if strings.Contains(query, "traffic_usage_hourly") {
				hourly++
			}
		}
		if hourly != 1 {
			t.Fatalf("want one projection scan, got %d: %v", hourly, trace.queries)
		}
	}
	assert(0, 0)
	ctx := context.Background()
	// The open UTC hour is visible as soon as the accounting batch commits.
	current := accountingBenchmarkEvents(credential, 0, 32)
	if err := h.projectZeroNodeEvents(ctx, current); err != nil {
		t.Fatal(err)
	}
	assert(960, 960)
	if err := h.projectZeroNodeEvents(ctx, current); err != nil {
		t.Fatal(err)
	}
	assert(960, 960)
	// Late delivery updates its historical bucket without waiting for a job.
	late := accountingBenchmarkEvents(credential, 1, 32)
	for i := range late {
		late[i].OccurredAt = time.Now().UTC().Truncate(24 * time.Hour).Add(-time.Hour)
	}
	if err := h.projectZeroNodeEvents(ctx, late); err != nil {
		t.Fatal(err)
	}
	assert(1920, 960)
	abort := errors.New("abort accounting transaction")
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		record := model.TrafficRecord{UserID: subscription.UserID, SubscriptionID: credential.SubscriptionID, NodeID: credential.NodeID, ProtocolEndpointID: credential.ProtocolEndpointID, ReportID: "rolled-back", Nonce: "rolled-back", UsedBytes: 100, At: time.Now().UTC()}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{record}); err != nil {
			return err
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	assert(1920, 960)
}

func TestTrafficRecordsCountsAndSumsInOneScan(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	f.capture()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	out, err := f.h.services.Records().Read(context.Background(), 1, metering.RecordsQuery{Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 2})
	if err != nil || out.Total != 6 || out.Aggregates.UsedBytes != 210 || len(out.Records) != 2 {
		t.Fatalf("records: %+v: %v", out, err)
	}
	scans := 0
	for _, query := range f.log.queries {
		if strings.Contains(query, "FROM `traffic_records`") {
			scans++
		}
	}
	if scans != 2 {
		t.Fatalf("want one statistics scan and one bounded page, got %d: %v", scans, f.log.queries)
	}
}

type summarySettlementDuringRead struct {
	logger.Interface
	settle func()
}

func (l *summarySettlementDuringRead) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if l.settle != nil && strings.Contains(sql, "FROM `traffic_usage_hourly`") {
		settle := l.settle
		l.settle = nil
		settle()
	}
}

func TestTrafficSummarySnapshotDoesNotBlockOrMixConcurrentSettlement(t *testing.T) {
	f := newTrafficReadFixture(t)
	subscription := model.Subscription{ID: 1, UserID: 1, Status: "active", FlowTotal: 1000, FlowUsed: 30, EndAt: time.Now().UTC().Add(24 * time.Hour)}
	if err := f.h.db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	record := model.TrafficRecord{UserID: 1, NodeID: 1, SubscriptionID: 1, ReportID: "before", Nonce: "before", UsedBytes: 30, At: time.Now().UTC()}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	closeRead, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeRead() })
	committed := false
	trace := &summarySettlementDuringRead{Interface: logger.Discard, settle: func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&subscription).Update("flow_used", 100).Error; err != nil {
				return err
			}
			record := model.TrafficRecord{UserID: 1, NodeID: 1, SubscriptionID: 1, ReportID: "during", Nonce: "during", UsedBytes: 70, At: time.Now().UTC()}
			return tx.Create(&record).Error
		}); err != nil {
			t.Fatal("reporting blocked settlement:", err)
		}
		committed = true
	}}
	read := f.h.services.PrincipalTrends().Repository.(meteringstore.PrincipalTrends).ReadDB
	f.h.services.SetTrafficReadDatabaseForTest(read.Session(&gorm.Session{Logger: trace}))
	service := f.h.services.UsageSummary()
	first, err := service.Read(context.Background(), 1, metering.UsageSummaryQuery{})
	if err != nil || !committed || first.TotalUsedBytes != 30 || first.RemainingBytes != 970 {
		t.Fatalf("summary mixed snapshots: %+v: %v", first, err)
	}
	next, err := service.Read(context.Background(), 1, metering.UsageSummaryQuery{})
	if err != nil || next.TotalUsedBytes != 100 || next.RemainingBytes != 900 {
		t.Fatalf("summary did not observe committed settlement: %+v: %v", next, err)
	}
}
