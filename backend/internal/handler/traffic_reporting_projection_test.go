package handler

import (
	"context"
	"errors"
	"strconv"
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
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
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
		drainUsageProjections(t, h.db)
		trace.queries = nil
		out, err := h.services.UsageSummary().Read(context.Background(), subscription.UserID, metering.UsageSummaryQuery{})
		if err != nil || out.TotalUsedBytes != total || out.UsedBytesToday != today || out.RemainingBytes != subscription.FlowTotal-total {
			t.Fatalf("live summary: %+v: %v", out, err)
		}
		hourly := 0
		for _, query := range trace.queries {
			if strings.Contains(query, "traffic_records") && !strings.Contains(query, "JOIN traffic_records r ON r.id=p.traffic_record_id") && !strings.Contains(query, "WHERE id IN") {
				t.Fatalf("summary scanned history instead of a bounded checkpoint join: %s", query)
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
	// The open UTC hour is exact from settlements; closed history uses the worker.
	current := accountingBenchmarkEvents(credential, 0, 32)
	if err := h.projectZeroNodeEvents(ctx, current); err != nil {
		t.Fatal(err)
	}
	assert(960, 960)
	if err := h.projectZeroNodeEvents(ctx, current); err != nil {
		t.Fatal(err)
	}
	assert(960, 960)
	// Late delivery updates its historical bucket through the same worker.
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
	const evidence = "retained legacy full evidence"
	if err := f.h.db.Model(&model.TrafficRecord{}).Where("user_id = ?", 1).Update("meta", evidence).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// A supplied foreign user scope cannot override the authenticated actor.
	q := metering.RecordsQuery{UserID: 2, Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 2}
	service := f.h.services.Records()
	for _, full := range []bool{false, true} {
		name := "summary"
		if full {
			name = "full_evidence"
		}
		t.Run(name, func(t *testing.T) {
			f.capture()
			if full {
				out, err := service.Read(t.Context(), 1, q)
				if err != nil || out.Total != 6 || out.Aggregates.RawBytes != 210 || out.Aggregates.UsedBytes != 210 || len(out.Records) != 2 || !out.HasMore {
					t.Fatalf("full records: %+v: %v", out, err)
				}
				if out.Records[0].ID != 2 || out.Records[1].ID != 6 || out.Records[0].UserID != 1 || out.Records[1].UserID != 1 || out.Records[0].Meta != evidence || out.Records[1].Meta != evidence {
					t.Fatal("full page lost ordering, owner scope or evidence", out.Records)
				}
			} else {
				out, err := service.ReadSummaryPage(t.Context(), 1, q)
				if err != nil || out.Total != 6 || out.Aggregates.RawBytes != 210 || out.Aggregates.UsedBytes != 210 || len(out.Records) != 2 || !out.HasMore {
					t.Fatalf("summary records: %+v: %v", out, err)
				}
				if out.Records[0].ID != 2 || out.Records[1].ID != 6 || out.Records[0].UserID != 1 || out.Records[1].UserID != 1 {
					t.Fatal("summary page changed ordering or owner scope", out.Records)
				}
			}
			assertTrafficRecordScanContract(t, f.log.queries, full, q.Limit)
		})
	}
}

func assertTrafficRecordScanContract(t testing.TB, captured []string, full bool, limit int) {
	t.Helper()
	queries := make([]string, 0, len(captured))
	for _, raw := range captured {
		query := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(raw, "`", ""))), " ")
		if !strings.Contains(query, " from traffic_records ") {
			t.Fatal("record read introduced unrelated material/table access", raw)
		}
		if !strings.Contains(query, "user_id = 1") || !strings.Contains(query, "record_at >= '2026-09-01 00:00:00'") || !strings.Contains(query, "record_at < '2026-09-02 00:00:00'") {
			t.Fatal("a statistics, metadata or page read escaped the same owner/time scope", raw)
		}
		queries = append(queries, query)
	}
	want := 2
	if full {
		want = 3
	}
	if len(queries) != want {
		t.Fatalf("want one statistics scan plus explicit bounded page stages (full=%t), got %v", full, captured)
	}
	stats := queries[0]
	if !strings.HasPrefix(stats, "select count(*) as total,") || !strings.Contains(stats, "sum(raw_bytes)") || !strings.Contains(stats, "sum(used_bytes)") || strings.Contains(stats, " limit ") || strings.Contains(stats, " order by ") {
		t.Fatal("range-wide count and amounts must share exactly one aggregate scan", captured)
	}
	for _, page := range queries[1:] {
		if strings.Contains(page, "count(") || strings.Contains(page, "sum(") || !strings.HasSuffix(page, "order by record_at desc, id desc limit "+strconv.Itoa(limit+1)) {
			t.Fatal("page/metadata must use the same limit+1 seek ordering without another aggregate scan", captured)
		}
	}
	if full {
		if !strings.HasPrefix(queries[1], "select id,event_type,coalesce(octet_length(meta),0) as meta_bytes from traffic_records ") || !strings.HasPrefix(queries[2], "select * from traffic_records ") {
			t.Fatal("bounded thin length preflight must precede full evidence loading", captured)
		}
	} else {
		for _, query := range queries {
			if strings.Contains(query, "meta") || strings.Contains(query, "select *") || strings.Contains(query, "event_type") {
				t.Fatal("core summary touched full proof, metadata preflight or archive material", captured)
			}
		}
		if !strings.HasPrefix(queries[1], "select id,user_id,subscription_id,node_id,protocol_endpoint_id,raw_bytes,upload_bytes,download_bytes,protocol_multiplier_milli,used_bytes,record_at,is_aggregate,record_count from traffic_records ") {
			t.Fatal("summary no longer selects its explicit narrow projection", captured)
		}
	}
}

type recordSettlementDuringRead struct {
	logger.Interface
	settle func()
}

func (l *recordSettlementDuringRead) Trace(_ context.Context, _ time.Time, statement func() (string, int64), _ error) {
	query, _ := statement()
	if l.settle != nil && strings.Contains(query, "COUNT(*) AS total") && strings.Contains(query, "traffic_records") {
		settle := l.settle
		l.settle = nil
		settle()
	}
}

func TestTrafficRecordsSummaryAndFullKeepSnapshotAcrossConcurrentSettlement(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "summary"
		if full {
			name = "full_evidence"
		}
		t.Run(name, func(t *testing.T) {
			f := newTrafficReadFixture(t)
			f.seedUsage(t)
			closeRead, err := f.h.ConfigureTrafficReads()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = closeRead() })
			from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			q := metering.RecordsQuery{Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 2}
			committed := false
			trace := &recordSettlementDuringRead{Interface: logger.Discard, settle: func() {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				row := model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ProtocolEndpointID: 1, ReportID: "during-record-page", Nonce: "during-record-page", RawBytes: 70, UsedBytes: 70, At: from.Add(2*time.Hour + 2*time.Minute), Meta: "concurrently committed legacy evidence"}
				if err := f.h.db.WithContext(ctx).Create(&row).Error; err != nil {
					t.Fatal("record read snapshot blocked independent settlement", err)
				}
				committed = true
			}}
			read := f.h.services.PrincipalTrends().Repository.(meteringstore.PrincipalTrends).ReadDB
			f.h.services.SetTrafficReadDatabaseForTest(read.Session(&gorm.Session{Logger: trace}))
			service := f.h.services.Records()
			readPage := func(wantTotal, wantUsed int64, wantFirst uint) {
				t.Helper()
				if full {
					out, err := service.Read(t.Context(), 1, q)
					if err != nil || out.Total != wantTotal || out.Aggregates.UsedBytes != wantUsed || len(out.Records) != 2 || out.Records[0].ID != wantFirst || !out.HasMore {
						t.Fatal("full page mixed statistics/preflight/evidence snapshots", out, err)
					}
				} else {
					out, err := service.ReadSummaryPage(t.Context(), 1, q)
					if err != nil || out.Total != wantTotal || out.Aggregates.UsedBytes != wantUsed || len(out.Records) != 2 || out.Records[0].ID != wantFirst || !out.HasMore {
						t.Fatal("summary mixed statistics/page snapshots", out, err)
					}
				}
			}
			readPage(6, 210, 2)
			if !committed {
				t.Fatal("concurrent commit did not execute between statistics and page loading")
			}
			readPage(7, 280, 8)
		})
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
	rebuildFixtureHourly(t, f.h.db)
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
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{record})
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
		t.Fatalf("current-hour settlement was hidden behind its pending projection: %+v %v", next, err)
	}
	quota, err := f.h.services.UsageQuota().Read(context.Background(), 1, metering.UsageSummaryQuery{})
	if err != nil || quota.RemainingBytes != 900 {
		t.Fatal("current quota waited on statistics", quota, err)
	}
	drainUsageProjections(t, f.h.db)
	next, err = service.Read(context.Background(), 1, metering.UsageSummaryQuery{})
	if err != nil || next.TotalUsedBytes != 100 || next.RemainingBytes != 900 {
		t.Fatalf("summary did not observe committed settlement: %+v: %v", next, err)
	}
}
