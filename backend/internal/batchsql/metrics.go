// Package batchsql observes only explicitly scoped native authority transactions.
// It never retains SQL, parameters, subject IDs or business decisions.
package batchsql

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const SampleCapacity = 64

var callbackFailures atomic.Uint64

type contextKey struct{}

var batchContextKey = &contextKey{}

type Counts struct {
	Statements                uint64 `json:"statement_count"`
	Reads                     uint64 `json:"read_count"`
	Writes                    uint64 `json:"write_count"`
	UnknownStatements         uint64 `json:"unknown_statement_count"`
	StatementErrors           uint64 `json:"statement_errors"`
	ExpectedReadMisses        uint64 `json:"expected_read_misses"`
	UnknownResults            uint64 `json:"unknown_statement_results"`
	ObservedAffectedWriteRows uint64 `json:"observed_affected_write_rows"`
	UnknownWriteRows          uint64 `json:"unknown_write_rows"`
	TextBlobArgumentBytes     uint64 `json:"text_blob_bound_argument_bytes"`
	OtherArguments            uint64 `json:"non_text_blob_argument_count"`
	SourcePayloadBytes        uint64 `json:"source_payload_bytes"`
	CallbackNS                uint64 `json:"callback_nanoseconds"`
}
type Sample struct {
	Sequence           uint64    `json:"sequence"`
	StartedAt          time.Time `json:"started_at"`
	FinishedAt         time.Time `json:"finished_at"`
	Outcome            string    `json:"outcome"`
	CommitConfirmed    *bool     `json:"commit_confirmed"`
	StatisticsComplete bool      `json:"statistics_complete"`
	AffectedWriteRows  *uint64   `json:"affected_write_rows"`
	Counts
}
type Snapshot struct {
	CallbackFailures           uint64     `json:"callback_failures"`
	TotalsComplete             bool       `json:"totals_complete"`
	Scope                      string     `json:"scope"`
	Phase                      string     `json:"phase"`
	Known                      bool       `json:"known"`
	InstrumentationInstalled   bool       `json:"instrumentation_installed"`
	ResetAt                    time.Time  `json:"reset_at"`
	ObservedAt                 *time.Time `json:"observed_at"`
	FreshnessSeconds           float64    `json:"freshness_seconds"`
	SampleCapacity             int        `json:"sample_capacity"`
	FreshBatchCount            int        `json:"fresh_batch_count"`
	RetainedBatchCount         int        `json:"retained_batch_count"`
	DriverTransactionsIncluded bool       `json:"driver_begin_commit_included"`
	PhysicalBytesMeasured      bool       `json:"physical_bytes_measured"`
	Batches                    uint64     `json:"batches"`
	CommittedBatches           uint64     `json:"commit_confirmed_batches"`
	ErrorOrUnknownBatches      uint64     `json:"error_or_unknown_batches"`
	IncompleteBatches          uint64     `json:"incomplete_batches"`
	Totals                     Counts     `json:"totals"`
	Samples                    []Sample   `json:"samples"`
}
type Metrics struct {
	mu             sync.Mutex
	installed      bool
	reset          time.Time
	next           int
	count          int
	sequence       uint64
	committed      uint64
	unknown        uint64
	incomplete     uint64
	healthAtReset  uint64
	totalsComplete bool
	totals         Counts
	samples        [SampleCapacity]Sample
}
type Collector struct {
	mu     sync.Mutex
	owner  *Metrics
	sample Sample
	active uint64
	sealed bool
	health uint64
}

func (m *Metrics) Configure(installed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installed = installed
	if m.reset.IsZero() {
		m.reset = time.Now().UTC()
		m.healthAtReset = callbackFailures.Load()
		m.totalsComplete = true
	}
}
func (m *Metrics) Begin(ctx context.Context, sourceBytes uint64) (context.Context, *Collector) {
	if m == nil {
		return ctx, nil
	}
	m.mu.Lock()
	installed := m.installed
	m.mu.Unlock()
	if !installed {
		return ctx, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c := &Collector{owner: m, health: callbackFailures.Load(), sample: Sample{StartedAt: time.Now().UTC(), StatisticsComplete: true, Counts: Counts{SourcePayloadBytes: sourceBytes}}}
	return context.WithValue(ctx, batchContextKey, c), c
}
func saturating(value *uint64, n uint64) bool {
	if n > math.MaxUint64-*value {
		*value = math.MaxUint64
		return false
	}
	*value += n
	return true
}
func addCounts(dst *Counts, src Counts) bool {
	valid := true
	pairs := [][2]*uint64{{&dst.Statements, &src.Statements}, {&dst.Reads, &src.Reads}, {&dst.Writes, &src.Writes}, {&dst.UnknownStatements, &src.UnknownStatements}, {&dst.StatementErrors, &src.StatementErrors}, {&dst.ExpectedReadMisses, &src.ExpectedReadMisses}, {&dst.UnknownResults, &src.UnknownResults}, {&dst.ObservedAffectedWriteRows, &src.ObservedAffectedWriteRows}, {&dst.UnknownWriteRows, &src.UnknownWriteRows}, {&dst.TextBlobArgumentBytes, &src.TextBlobArgumentBytes}, {&dst.OtherArguments, &src.OtherArguments}, {&dst.SourcePayloadBytes, &src.SourcePayloadBytes}, {&dst.CallbackNS, &src.CallbackNS}}
	for _, pair := range pairs {
		if !saturating(pair[0], *pair[1]) {
			valid = false
		}
	}
	return valid
}
func (c *Collector) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.sample.StatisticsComplete = false
	c.mu.Unlock()
}
func (c *Collector) start() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sealed {
		callbackFailures.Add(1)
		return false
	}
	if c.active == math.MaxUint64 {
		c.sample.StatisticsComplete = false
		return false
	}
	c.active++
	return true
}
func (c *Collector) observe(count Counts) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sealed {
		callbackFailures.Add(1)
		return
	}
	if c.active == 0 {
		c.sample.StatisticsComplete = false
	} else {
		c.active--
	}
	if !addCounts(&c.sample.Counts, count) {
		c.sample.StatisticsComplete = false
	}
}

// true means the existing GORM Transaction returned nil after Commit. Any
// returned error or panic remains ambiguous: successful statements are still
// attempted rows/arguments, never silently reported as durable writes.
func (c *Collector) Finish(transactionErr error, returned bool) {
	defer func() {
		if recover() != nil {
			callbackFailures.Add(1)
		}
	}()
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.sealed {
		c.mu.Unlock()
		return
	}
	c.sealed = true
	c.sample.FinishedAt = time.Now().UTC()
	if c.active != 0 || c.health != callbackFailures.Load() {
		c.sample.StatisticsComplete = false
	}
	if returned && transactionErr == nil {
		v := true
		c.sample.CommitConfirmed = &v
		c.sample.Outcome = "commit_confirmed"
	} else if returned {
		c.sample.Outcome = "transaction_error_or_commit_unknown"
	} else {
		c.sample.Outcome = "panic_or_unfinished"
	}
	if c.sample.UnknownWriteRows == 0 {
		v := c.sample.ObservedAffectedWriteRows
		c.sample.AffectedWriteRows = &v
	}
	sample := c.sample
	c.mu.Unlock()
	m := c.owner
	m.mu.Lock()
	defer m.mu.Unlock()
	if !saturating(&m.sequence, 1) {
		sample.StatisticsComplete = false
	}
	sample.Sequence = m.sequence
	if sample.CommitConfirmed != nil && *sample.CommitConfirmed {
		saturating(&m.committed, 1)
	} else {
		saturating(&m.unknown, 1)
	}
	if !sample.StatisticsComplete {
		saturating(&m.incomplete, 1)
	}
	if !addCounts(&m.totals, sample.Counts) {
		saturating(&m.incomplete, 1)
		m.totalsComplete = false
	}
	m.samples[m.next] = sample
	m.next = (m.next + 1) % SampleCapacity
	m.count = min(m.count+1, SampleCapacity)
}
func (m *Metrics) Snapshot(now time.Time, freshness time.Duration) Snapshot {
	out := Snapshot{Scope: "this_process", Phase: "native_authority_transaction", FreshnessSeconds: freshness.Seconds(), SampleCapacity: SampleCapacity, Samples: []Sample{}}
	if m == nil {
		return out
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out.InstrumentationInstalled = m.installed
	out.ResetAt = m.reset
	out.Batches = m.sequence
	out.CommittedBatches = m.committed
	out.ErrorOrUnknownBatches = m.unknown
	out.IncompleteBatches = m.incomplete
	out.Totals = m.totals
	out.TotalsComplete = m.totalsComplete
	out.CallbackFailures = callbackFailures.Load() - m.healthAtReset
	out.RetainedBatchCount = m.count
	if freshness <= 0 || freshness > 5*time.Minute {
		return out
	}
	for i := 0; i < m.count; i++ {
		sample := m.samples[(m.next-m.count+i+SampleCapacity)%SampleCapacity]
		if out.ObservedAt == nil || sample.FinishedAt.After(*out.ObservedAt) {
			v := sample.FinishedAt
			out.ObservedAt = &v
		}
		if sample.FinishedAt.After(now) || now.Sub(sample.FinishedAt) > freshness {
			continue
		}
		out.Samples = append(out.Samples, sample)
		out.FreshBatchCount++
	}
	out.Known = m.installed && out.FreshBatchCount > 0 && out.CallbackFailures == 0 && m.totalsComplete
	for _, sample := range out.Samples {
		if !sample.StatisticsComplete {
			out.Known = false
		}
	}
	return out
}

func (c *Collector) MarkIncomplete() { c.invalidate() }
