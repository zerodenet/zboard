package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
}

func (l *accountingQueryCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	l.queries.Add(1)
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
			b.ReportMetric(float64(counter.queries.Load())/float64(b.N), "SQL/batch")
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
