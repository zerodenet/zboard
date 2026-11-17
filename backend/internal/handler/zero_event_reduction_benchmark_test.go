package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm/logger"
)

// Compare the same 1024 cumulative updates (32 active flows, 32 samples each).
// Includes parsing, accounting, node and coverage projections; excludes fixture
// construction, HTTP, durable spool append and checkpoint fsync.
func BenchmarkZeroEventReduction(b *testing.B) {
	for _, window := range []int{32, 2000} {
		b.Run(fmt.Sprintf("window_%d", window), func(b *testing.B) {
			h, credential := accountingBenchmarkFixture(b)
			counter := &accountingQueryCounter{Interface: logger.Default.LogMode(logger.Silent)}
			h.db.Logger = counter
			const count = 1024
			at := time.Date(2026, 10, 16, 5, 0, 1, 0, time.UTC)
			b.ReportAllocs()
			b.ResetTimer()
			for batch := 0; batch < b.N; batch++ {
				events := make([]zeroevent.Envelope, count)
				for i := range events {
					flow := fmt.Sprintf("flow-%02d", i%32)
					value := batch*32 + i/32 + 1
					events[i] = zeroevent.Envelope{ID: fmt.Sprintf("event-%d-%d", batch, i), NodeID: uint64(credential.NodeID), Type: "flow.updated", CoreInstanceID: "core", PrincipalKey: credential.PrincipalKey, Sequence: uint64(batch*count + i + 1), FlowID: flow, OccurredAt: at, ReceivedAt: at, Payload: json.RawMessage(fmt.Sprintf(`{"flow_id":%q,"traffic":{"bytes_up":%d,"bytes_down":%d}}`, flow, value, 2*value))}
				}
				for start := 0; start < count; start += window {
					if err := h.projectZeroNodeEvents(context.Background(), events[start:min(start+window, count)]); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(counter.queries.Load())/float64(b.N), "SQL/window")
			b.ReportMetric(float64(b.N*count)/b.Elapsed().Seconds(), "input-events/s")
			var subscription model.Subscription
			if err := h.db.First(&subscription, credential.SubscriptionID).Error; err != nil || subscription.FlowUsed != int64(b.N*count*3) {
				b.Fatalf("subscription=%+v err=%v", subscription, err)
			}
		})
	}
}
