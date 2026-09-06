package handler

import (
	"sync/atomic"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

const eventLatencyBucketMillis = 100

type zeroEventConsumerMetrics struct {
	batches   atomic.Uint64
	events    atomic.Uint64
	failures  atomic.Uint64
	processNS atomic.Uint64
	missing   atomic.Uint64
	maximumMS atomic.Uint64
	latencies [601]atomic.Uint64 // 100ms upper bounds through 60s plus overflow.
}

type zeroEventConsumerSnapshot struct {
	Batches             uint64  `json:"batches"`
	ProcessedEnvelopes  uint64  `json:"processed_envelopes"`
	Failures            uint64  `json:"failures"`
	ProcessingSeconds   float64 `json:"processing_seconds"`
	MissingReceiveTime  uint64  `json:"missing_receive_time"`
	LatencySamples      uint64  `json:"latency_samples"`
	LatencyBucketMillis int     `json:"latency_bucket_ms"`
	P95UpperMillis      *uint64 `json:"p95_upper_ms"`
	MaximumMillis       uint64  `json:"maximum_ms"`
}

func (m *zeroEventConsumerMetrics) committed(events []zeroevent.Envelope, elapsed time.Duration, now time.Time) {
	m.batches.Add(1)
	m.events.Add(uint64(len(events)))
	m.processNS.Add(uint64(max(elapsed, 0)))
	for _, event := range events {
		if event.ReceivedAt.IsZero() || event.ReceivedAt.After(now) {
			m.missing.Add(1)
			continue
		}
		age := now.Sub(event.ReceivedAt)
		milliseconds := uint64(age / time.Millisecond)
		if age%time.Millisecond != 0 {
			milliseconds++
		}
		for previous := m.maximumMS.Load(); milliseconds > previous; previous = m.maximumMS.Load() {
			if m.maximumMS.CompareAndSwap(previous, milliseconds) {
				break
			}
		}
		bucket := min((milliseconds+eventLatencyBucketMillis-1)/eventLatencyBucketMillis, uint64(len(m.latencies)-1))
		m.latencies[bucket].Add(1)
	}
}

func (m *zeroEventConsumerMetrics) snapshot() zeroEventConsumerSnapshot {
	result := zeroEventConsumerSnapshot{Batches: m.batches.Load(), ProcessedEnvelopes: m.events.Load(), Failures: m.failures.Load(),
		ProcessingSeconds: float64(m.processNS.Load()) / float64(time.Second), MissingReceiveTime: m.missing.Load(),
		LatencyBucketMillis: eventLatencyBucketMillis, MaximumMillis: m.maximumMS.Load()}
	var counts [601]uint64
	for i := range counts {
		counts[i] = m.latencies[i].Load()
		result.LatencySamples += counts[i]
	}
	// Writers update totals/max before histogram buckets. Load those counters
	// after the buckets so a concurrent sample cannot report more observations
	// than processed envelopes, or a maximum below an observed bucket.
	result.MissingReceiveTime = m.missing.Load()
	result.ProcessedEnvelopes = m.events.Load()
	result.Batches = m.batches.Load()
	result.ProcessingSeconds = float64(m.processNS.Load()) / float64(time.Second)
	result.MaximumMillis = m.maximumMS.Load()
	if result.LatencySamples > 0 {
		target, seen := (result.LatencySamples*95+99)/100, uint64(0)
		for i, count := range counts {
			seen += count
			if seen >= target {
				upper := uint64(i * eventLatencyBucketMillis)
				if i == len(counts)-1 {
					upper = max(upper, m.maximumMS.Load())
				}
				result.P95UpperMillis = &upper
				break
			}
		}
	}
	return result
}
