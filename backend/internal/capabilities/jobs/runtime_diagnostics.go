package jobs

import (
	"sort"
	"time"
)

const pressureClaim uint8 = 1
const pressureFinish uint8 = 2
const pressureEmptyClaim uint8 = 3

type DurableCallDiagnostics struct {
	Known            bool       `json:"known"`
	SampleCount      int        `json:"sample_count"`
	SuccessfulWrites int        `json:"successful_writes"`
	ErrorCount       int        `json:"error_count"`
	EmptyProbes      int        `json:"empty_probes"`
	ObservedAt       *time.Time `json:"observed_at"`
	MaximumMS        *float64   `json:"maximum_ms"`
	MeanMS           *float64   `json:"mean_ms"`
	P95MS            *float64   `json:"p95_ms"`
}
type DatabaseDiagnostics struct {
	Scope            string                 `json:"scope"`
	Timing           string                 `json:"timing"`
	FreshnessSeconds float64                `json:"freshness_seconds"`
	SampleCapacity   int                    `json:"sample_capacity"`
	Claim            DurableCallDiagnostics `json:"claim"`
	Finish           DurableCallDiagnostics `json:"finish"`
}

// These are measured durable Store calls, including their SQL and adapter
// logic. They exclude local admission wait and handler execution. They are NOT
// individual SQL statement, pool-wait-only or server-only latency measurements.
// The shared64-ring may evict either operation; counts never imply total calls.
func (r *Runtime) DatabaseDiagnostics(now time.Time, freshness time.Duration) DatabaseDiagnostics {
	out := DatabaseDiagnostics{Scope: "this_process", Timing: "durable_store_call", FreshnessSeconds: freshness.Seconds(), SampleCapacity: pressureWindow}
	if r == nil || freshness <= 0 || freshness > 5*time.Minute {
		return out
	}
	var samples [pressureWindow]pressureSample
	r.pressure.mu.Lock()
	samples = r.pressure.samples
	r.pressure.mu.Unlock()
	collect := func(operation uint8) DurableCallDiagnostics {
		var out DurableCallDiagnostics
		var values [pressureWindow]time.Duration
		var total float64
		var latest time.Time
		for _, sample := range samples {
			if sample.storage || (sample.operation != operation && !(operation == pressureClaim && sample.operation == pressureEmptyClaim)) || sample.at.IsZero() {
				continue
			}
			latest = maxObservationTime(latest, sample.at)
			if sample.at.After(now) || now.Sub(sample.at) > freshness {
				continue
			}
			values[out.SampleCount] = sample.latency
			out.SampleCount++
			total += float64(sample.latency)
			if sample.failed {
				out.ErrorCount++
			}
			if sample.write {
				out.SuccessfulWrites++
			}
			if sample.operation == pressureEmptyClaim {
				out.EmptyProbes++
			}
		}
		if !latest.IsZero() {
			v := latest.UTC()
			out.ObservedAt = &v
		}
		if out.SampleCount == 0 {
			return out
		}
		sort.Slice(values[:out.SampleCount], func(i, j int) bool { return values[i] < values[j] })
		maxMS := float64(values[out.SampleCount-1]) / float64(time.Millisecond)
		meanMS := float64(total) / float64(out.SampleCount) / float64(time.Millisecond)
		p95MS := float64(values[(out.SampleCount*95+99)/100-1]) / float64(time.Millisecond)
		out.Known = true
		out.MaximumMS = &maxMS
		out.MeanMS = &meanMS
		out.P95MS = &p95MS
		return out
	}
	out.Claim = collect(pressureClaim)
	out.Finish = collect(pressureFinish)
	return out
}
