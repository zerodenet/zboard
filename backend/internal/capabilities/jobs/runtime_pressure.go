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
	ObservedAt        time.Time
	Writes            int
	MaxLatency        time.Duration
	Failed            bool
	StorageObservedAt time.Time
	MaxStorageLatency time.Duration
	StorageActive     int
	StorageFailed     bool
	StorageUnknown    bool
}

type pressureSample struct {
	at      time.Time
	latency time.Duration
	write   bool
	failed  bool
	storage bool
}

type runtimePressure struct {
	mu              sync.Mutex
	next            int
	samples         [pressureWindow]pressureSample
	failedAt        time.Time
	storageFailedAt time.Time
	storageActive   [8]time.Time
	storageOverflow uint64
}

func (p *runtimePressure) record(start, finish time.Time, write bool, err error) {
	p.recordKind(start, finish, write, err, false)
}

func (p *runtimePressure) recordKind(start, finish time.Time, write bool, err error, storage bool) {
	if finish.Before(start) {
		return
	}
	p.mu.Lock()
	p.samples[p.next] = pressureSample{at: finish, latency: finish.Sub(start), write: write && err == nil, failed: err != nil, storage: storage}
	if err != nil {
		if storage {
			p.storageFailedAt = finish
		} else {
			p.failedAt = finish
		}
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
	out.StorageUnknown = r.pressure.storageOverflow != 0
	if !r.pressure.storageFailedAt.IsZero() && !r.pressure.storageFailedAt.After(now) && now.Sub(r.pressure.storageFailedAt) <= freshness {
		out.StorageFailed, out.StorageObservedAt = true, r.pressure.storageFailedAt
	}
	for _, started := range r.pressure.storageActive {
		if started.IsZero() {
			continue
		}
		out.StorageActive++
		if started.After(now) {
			out.StorageUnknown = true
			continue
		}
		out.StorageObservedAt = now
		out.MaxStorageLatency = max(out.MaxStorageLatency, now.Sub(started))
	}
	for _, sample := range r.pressure.samples {
		if sample.at.IsZero() || sample.at.After(now) || now.Sub(sample.at) > freshness {
			continue
		}
		if sample.storage {
			out.StorageObservedAt = maxObservationTime(out.StorageObservedAt, sample.at)
			out.StorageFailed = out.StorageFailed || sample.failed
			out.MaxStorageLatency = max(out.MaxStorageLatency, sample.latency)
		} else {
			out.ObservedAt = maxObservationTime(out.ObservedAt, sample.at)
			out.Failed = out.Failed || sample.failed
			out.MaxLatency = max(out.MaxLatency, sample.latency)
		}
		if sample.write {
			out.Writes++
			out.ObservedAt = maxObservationTime(out.ObservedAt, sample.at)
		}
	}
	return out
}

func maxObservationTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

// BeginStorageObservation observes a specific projection storage capability,
// not arbitrary plugin/handler execution. It includes in-flight time so a
// blocked row lock cannot look healthy until it eventually finishes. Storage
// work latency is reported separately from known durable Claim/Finish latency.
// The caller must finish on every return, panic and cancellation path.
func (r *Runtime) BeginStorageObservation() func(bool, error) {
	if r == nil {
		return func(bool, error) {}
	}
	started := time.Now()
	r.pressure.mu.Lock()
	slot := -1
	for i := range r.pressure.storageActive {
		if r.pressure.storageActive[i].IsZero() {
			slot = i
			r.pressure.storageActive[i] = started
			break
		}
	}
	if slot < 0 {
		r.pressure.storageOverflow++
	}
	r.pressure.mu.Unlock()
	var once sync.Once
	return func(changed bool, err error) {
		once.Do(func() {
			r.pressure.mu.Lock()
			if slot >= 0 {
				r.pressure.storageActive[slot] = time.Time{}
			} else {
				r.pressure.storageOverflow--
			}
			r.pressure.mu.Unlock()
			r.pressure.recordKind(started, time.Now(), changed, err, true)
		})
	}
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
