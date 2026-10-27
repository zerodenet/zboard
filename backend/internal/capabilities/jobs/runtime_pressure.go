package jobs

import (
	"context"
	"errors"
	"sync"
	"time"
)

const pressureWindow = 64

// DatabasePressure describes this runtime's actual durable admission/completion
// calls. It excludes handler time, empty probes, and in-memory admission waits.
// A recent error is retained even if a later statement succeeds. It is an
// observation for local capacity control, never a new authority or lease.
type DatabasePressure struct {
	ObservedAt time.Time
	Writes     int
	MaxLatency time.Duration
	Failed     bool
}

type pressureSample struct {
	at      time.Time
	latency time.Duration
	write   bool
	failed  bool
}

type runtimePressure struct {
	mu       sync.Mutex
	next     int
	samples  [pressureWindow]pressureSample
	failedAt time.Time
}

func (p *runtimePressure) record(start, finish time.Time, write bool, err error) {
	if finish.Before(start) {
		return
	}
	p.mu.Lock()
	p.samples[p.next] = pressureSample{at: finish, latency: finish.Sub(start), write: write && err == nil, failed: err != nil}
	if err != nil {
		p.failedAt = finish
	}
	p.next = (p.next + 1) % len(p.samples)
	p.mu.Unlock()
}

// DatabasePressure returns only samples inside the caller's bounded freshness
// window. Unknown/stale observations stay empty instead of becoming zero load.
func (r *Runtime) DatabasePressure(now time.Time, freshness time.Duration) DatabasePressure {
	var out DatabasePressure
	if freshness <= 0 || freshness > 5*time.Minute {
		return out
	}
	r.pressure.mu.Lock()
	defer r.pressure.mu.Unlock()
	if !r.pressure.failedAt.IsZero() && !r.pressure.failedAt.After(now) && now.Sub(r.pressure.failedAt) <= freshness {
		out.Failed = true
		out.ObservedAt = r.pressure.failedAt
	}
	for _, sample := range r.pressure.samples {
		if sample.at.IsZero() || sample.at.After(now) || now.Sub(sample.at) > freshness {
			continue
		}
		if out.ObservedAt.Before(sample.at) {
			out.ObservedAt = sample.at
		}
		if sample.write {
			out.Writes++
		}
		out.Failed = out.Failed || sample.failed
		if sample.latency > out.MaxLatency {
			out.MaxLatency = sample.latency
		}
	}
	return out
}

func (s runtimeExecutionStore) Finish(ctx context.Context, claim Claim, state State) error {
	start := time.Now()
	err := s.Store.Finish(ctx, claim, state)
	if s.runtime != nil {
		s.runtime.pressure.record(start, time.Now(), true, err)
	}
	return err
}

func (r *Runtime) observeClaim(start time.Time, claim Claim, err error) {
	// ErrEmpty can include a fast read-only probe and cannot demonstrate spare
	// write capacity. Slow empty probes remain useful pressure observations.
	if errors.Is(err, ErrEmpty) {
		r.pressure.record(start, time.Now(), false, nil)
		return
	}
	r.pressure.record(start, time.Now(), claim.Token != "", err)
}
