package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type accountingQueryCounter struct {
	logger.Interface
	queries atomic.Uint64
	reads   atomic.Uint64
	writes  atomic.Uint64
	rows    atomic.Uint64
	unknown atomic.Uint64
	errors  atomic.Uint64
	totalNS atomic.Uint64
	maxNS   atomic.Uint64
	latency [1001]atomic.Uint64 // 100us upper bounds through 100ms, then overflow.
}

func (l *accountingQueryCounter) Trace(_ context.Context, begin time.Time, statement func() (string, int64), err error) {
	// Benchmark-only observer. Retain numeric totals, never SQL text or values.
	elapsed := uint64(max(time.Since(begin), 0))
	text, rows := statement()
	fields := strings.Fields(text)
	if len(fields) > 0 {
		switch strings.ToUpper(fields[0]) {
		case "SELECT":
			l.reads.Add(1)
		case "INSERT", "UPDATE", "DELETE", "REPLACE":
			l.writes.Add(1)
			if rows < 0 {
				l.unknown.Add(1)
			} else {
				l.rows.Add(uint64(rows))
			}
		}
	}
	if err != nil {
		l.errors.Add(1)
	}
	l.queries.Add(1)
	l.totalNS.Add(elapsed)
	for previous := l.maxNS.Load(); elapsed > previous; previous = l.maxNS.Load() {
		if l.maxNS.CompareAndSwap(previous, elapsed) {
			break
		}
	}
	const width = uint64(100 * time.Microsecond)
	l.latency[min((elapsed+width-1)/width, uint64(len(l.latency)-1))].Add(1)
}

func (l *accountingQueryCounter) report(b *testing.B) {
	n := float64(b.N)
	queries := l.queries.Load()
	b.ReportMetric(float64(queries)/n, "SQL/batch")
	b.ReportMetric(float64(l.reads.Load())/n, "SQL-reads/batch")
	b.ReportMetric(float64(l.writes.Load())/n, "SQL-writes/batch")
	b.ReportMetric(float64(l.rows.Load())/n, "affected-write-rows/batch")
	b.ReportMetric(float64(l.unknown.Load())/n, "unknown-write-rows/batch")
	b.ReportMetric(float64(l.errors.Load())/n, "SQL-errors/batch")
	if queries == 0 {
		return
	}
	b.ReportMetric(float64(l.totalNS.Load())/float64(queries)/1e3, "SQL-mean-us")
	var seen uint64
	for i := range l.latency {
		seen += l.latency[i].Load()
		if seen >= (queries*95+99)/100 {
			upper := uint64(i) * uint64(100*time.Microsecond)
			if i == len(l.latency)-1 {
				upper = max(upper, l.maxNS.Load())
			}
			b.ReportMetric(float64(upper)/1e3, "SQL-p95-upper-us")
			break
		}
	}
}

func accountingBenchmarkFixture(t testing.TB) (*handlers, model.ProtocolCredential) {
	t.Helper()
	f := newOrderFixture(t)
	return accountingFixtureFromOrder(t, f)
}

func accountingFixtureFromOrder(t testing.TB, f orderFixture) (*handlers, model.ProtocolCredential) {
	t.Helper()
	// RunMigrations creates tables; production startup also reconciles the
	// reporting indexes. Include their write cost in this accounting fixture.
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Update("traffic_bytes", int64(1<<60)).Error; err != nil {
		t.Fatal(err)
	}
	order := f.paid(t, f.create(t, 0).ID)
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", order.SubscriptionID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	f.h.db.Logger = logger.Default.LogMode(logger.Silent)
	return f.h, credential
}

func accountingBenchmarkEvents(credential model.ProtocolCredential, batch, count int) []zeroevent.Envelope {
	events := make([]zeroevent.Envelope, count)
	at := time.Now().UTC()
	for i := range events {
		flow := fmt.Sprintf("flow-%d", i)
		events[i] = zeroevent.Envelope{
			ID: fmt.Sprintf("accounting-%d-%d", batch, i), NodeID: uint64(credential.NodeID),
			Type: "flow.updated", CoreInstanceID: "benchmark-core", PrincipalKey: credential.PrincipalKey,
			Sequence: uint64(batch*count + i + 1), FlowID: flow, OccurredAt: at, ReceivedAt: at,
			Payload: json.RawMessage(fmt.Sprintf(`{"flow_id":%q,"traffic":{"bytes_up":%d,"bytes_down":%d}}`, flow, (batch+1)*10, (batch+1)*20)),
		}
	}
	return events
}

// One operation is a committed batch of 32 distinct flows for one subscription.
// Includes envelope generation, parsing and database writes; excludes setup and
// final assertions. CoverageOnly is a separate measurement, not a subtraction
// claimed to represent core-only latency. Use fixed -benchtime=100x for comparisons.
// SQL timings are GORM client durations (not server-only latency); counts exclude
// driver BEGIN/COMMIT. Affected rows are logical attempted DML rows, not physical
// database/WAL bytes. Successful benchmarks contain no rolled-back statements.
func BenchmarkZeroAccounting(b *testing.B) {
	for _, mode := range []string{"FullBatch", "CoverageOnly"} {
		b.Run(mode, func(b *testing.B) {
			h, credential := accountingBenchmarkFixture(b)
			counter := &accountingQueryCounter{Interface: logger.Default.LogMode(logger.Silent)}
			h.db.Logger = counter
			const batchSize = 32
			durations := make([]time.Duration, 0, min(b.N, 10000))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				started := time.Now()
				events := accountingBenchmarkEvents(credential, i, batchSize)
				var err error
				if mode == "FullBatch" {
					err = h.projectZeroNodeEvents(context.Background(), events)
				} else {
					err = h.db.Transaction(func(tx *gorm.DB) error { return h.projectFairUseCoverageBatch(tx, events) })
				}
				if err != nil {
					b.Fatal(err)
				}
				elapsed := time.Since(started)
				if len(durations) < cap(durations) {
					durations = append(durations, elapsed)
				} else {
					durations[i%len(durations)] = elapsed
				}
			}
			b.StopTimer()
			counter.report(b)
			b.ReportMetric(float64(b.N*batchSize)/b.Elapsed().Seconds(), "events/s")
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			b.ReportMetric(float64(durations[(len(durations)-1)*95/100].Nanoseconds())/1e6, "p95-ms/batch")
			b.ReportMetric(float64(durations[(len(durations)-1)*99/100].Nanoseconds())/1e6, "p99-ms/batch")
			if mode == "FullBatch" {
				// Replay the last batch after measurement. Totals must remain exact.
				if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, b.N-1, batchSize)); err != nil {
					b.Fatal(err)
				}
				var subscription model.Subscription
				if err := h.db.First(&subscription, credential.SubscriptionID).Error; err != nil {
					b.Fatal(err)
				}
				if want := int64(b.N * batchSize * 30); subscription.FlowUsed != want {
					b.Fatalf("charged %d, want %d", subscription.FlowUsed, want)
				}
				var total int64
				if err := h.db.Model(&model.TrafficRecord{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&total).Error; err != nil {
					b.Fatal(err)
				}
				if total != subscription.FlowUsed {
					b.Fatalf("traffic ledger=%d subscription=%d", total, subscription.FlowUsed)
				}
			}
		})
	}
}
