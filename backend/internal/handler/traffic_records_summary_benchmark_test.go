package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type rawSummaryBenchLog struct {
	logger.Interface
	selects, dml int64
	capture      bool
	queries      []string
}

func (l *rawSummaryBenchLog) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if l.capture {
		l.queries = append(l.queries, sql)
	}
	switch {
	case strings.HasPrefix(sql, "SELECT"):
		l.selects++
	case strings.HasPrefix(sql, "INSERT"), strings.HasPrefix(sql, "UPDATE"), strings.HasPrefix(sql, "DELETE"):
		l.dml++
	}
}

type rawSummaryBenchPage struct {
	Items      any                       `json:"items"`
	Total      int64                     `json:"total"`
	Aggregates metering.RecordAggregates `json:"aggregates"`
	HasMore    bool                      `json:"has_more"`
}

var rawSummaryBenchSink rawSummaryBenchPage

// Reproducible benchmark. Measures a bounded SQLite read/conversion
// slice, including identical authority/statistics reads. Excludes fixture setup,
// JSON serialization, outer HTTP authentication and any core accounting writer.
func BenchmarkTrafficRecordsRawSummaryLargeMeta(b *testing.B) {
	h, _ := newAnnouncementTestHandlers(b)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	prefix := `{"accounting_samples":1,"first_sequence":1,"rounding_numerator":500,"diagnostic":"`
	meta := prefix + strings.Repeat("m", 32*1024-len(prefix)-2) + `"}`
	if len(meta) != 32*1024 {
		b.Fatal("wrong bounded metadata fixture")
	}
	facts := make([]model.TrafficRecord, 0, 203)
	for i := 0; i < 203; i++ {
		at := from.Add(time.Duration(i) * time.Minute)
		if i == 201 {
			at = from.Add(-time.Second)
		} else if i == 202 {
			at = from.Add(24 * time.Hour)
		}
		facts = append(facts, model.TrafficRecord{UserID: 1, SubscriptionID: 11, NodeID: 7, ProtocolEndpointID: 9, EntitlementEpoch: 5, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i), At: at, RawBytes: 7, UploadBytes: 3, DownloadBytes: 4, UsedBytes: 11, TrafficCalcMode: 0, ProtocolMultiplierMilli: 1500, Meta: meta})
	}
	if err := h.db.CreateInBatches(&facts, 100).Error; err != nil {
		b.Fatal(err)
	}
	trace := &rawSummaryBenchLog{Interface: logger.Discard}
	s := metering.Records{Repository: meteringstore.Records{DB: h.db.Session(&gorm.Session{Logger: trace})}}
	ctx := context.Background()
	q := metering.RecordsQuery{UserID: 1, SubscriptionID: 11, NodeID: 7, ProtocolEndpointID: 9, Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 200}
	old := func() (rawSummaryBenchPage, error) {
		full, err := s.Read(ctx, 1, q)
		if err != nil {
			return rawSummaryBenchPage{}, err
		}
		// Exact old raw HTTP conversion, including the intermediate model copy.
		rows := make([]model.TrafficRecord, 0, len(full.Records))
		for _, record := range full.Records {
			rows = append(rows, model.TrafficRecord(record))
		}
		return rawSummaryBenchPage{Items: trafficRecordSummaries(rows), Total: full.Total, Aggregates: full.Aggregates, HasMore: full.HasMore}, nil
	}
	current := func() (rawSummaryBenchPage, error) {
		page, err := s.ReadSummaryPage(ctx, 1, q)
		return rawSummaryBenchPage{Items: page.Records, Total: page.Total, Aggregates: page.Aggregates, HasMore: page.HasMore}, err
	}
	// Capture structural assertions outside timed benchmark loops. Full reads
	// require their bounded thin preflight; summaries must continue to avoid it.
	checkReadShape := func(full bool) {
		b.Helper()
		var records []string
		auth := 0
		for _, query := range trace.queries {
			if strings.Contains(query, "FROM `users`") {
				auth++
				continue
			}
			records = append(records, query)
		}
		if auth != 1 {
			b.Fatal("record read must authorize exactly once", trace.queries)
		}
		// Validate the actual SQL with this benchmark's limit; do not rewrite
		// queries or hide extra reads to fit the smaller regression fixture.
		assertTrafficRecordScanContract(b, records, full, q.Limit)
	}
	trace.capture = true
	before, err := old()
	if err != nil || before.Total != 201 || before.Aggregates.UsedBytes != 2211 || !before.HasMore || len(before.Items.([]trafficRecordListItem)) != 200 {
		b.Fatalf("old output check: %+v %v", before, err)
	}
	checkReadShape(true)
	trace.queries = nil
	after, err := current()
	if err != nil {
		b.Fatal(err)
	}
	checkReadShape(false)
	trace.capture = false
	trace.queries = nil
	a, err := json.Marshal(before)
	if err != nil {
		b.Fatal(err)
	}
	z, err := json.Marshal(after)
	if err != nil || !reflect.DeepEqual(a, z) {
		b.Fatal("old/new summary output differs", err)
	}
	checkFacts := func() {
		b.Helper()
		var totals struct{ Records, Used, MetaBytes, MinMeta, MaxMeta int64 }
		if err := h.db.Model(&model.TrafficRecord{}).Select("COUNT(*) AS records, SUM(used_bytes) AS used, SUM(length(meta)) AS meta_bytes, MIN(length(meta)) AS min_meta, MAX(length(meta)) AS max_meta").Scan(&totals).Error; err != nil || totals.Records != 203 || totals.Used != 2233 || totals.MetaBytes != 203*32*1024 || totals.MinMeta != 32*1024 || totals.MaxMeta != 32*1024 {
			b.Fatalf("source facts/meta changed: %+v %v", totals, err)
		}
	}
	checkFacts()
	for _, sample := range []struct {
		name    string
		run     func() (rawSummaryBenchPage, error)
		selects int64
	}{{"old_full_read_and_http_summary", old, 4}, {"new_explicit_summary_page", current, 3}} {
		b.Run(sample.name, func(b *testing.B) {
			trace.selects, trace.dml = 0, 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := sample.run()
				if err != nil {
					b.Fatal(err)
				}
				rawSummaryBenchSink = out
			}
			b.StopTimer()
			if trace.selects != sample.selects*int64(b.N) || trace.dml != 0 {
				b.Fatalf("read SQL contract changed: selects=%d dml=%d", trace.selects, trace.dml)
			}
			b.ReportMetric(float64(trace.selects)/float64(b.N), "SELECT/op")
			b.ReportMetric(float64(trace.dml)/float64(b.N), "DML/op")
			checkFacts()
		})
	}
}
