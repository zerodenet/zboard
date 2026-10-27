// Package localworkers controls optional, same-host projection processes. The
// existing durable job store remains the sole execution and financial authority.
package localworkers

import (
	"errors"
	"math"
	"time"
)

type Policy struct {
	MaxWorkers                    int
	AdministratorConfirmedBenefit bool
	Cooldown                      time.Duration
	IdleFor                       time.Duration
	BacklogDelay                  time.Duration
	Freshness                     time.Duration
	MinHealthySamples             int
	MinDatabaseWrites             int
	MaxDatabaseLatency            time.Duration
	MaxCPUFraction                float64
	MaxIOWaitFraction             float64
	WorkerCPUReserve              float64
	ReserveMemoryBytes            uint64
	WorkerMemoryBytes             uint64
	ReserveDiskBytes              uint64
}

func DefaultPolicy() Policy {
	return Policy{MaxWorkers: 0, Cooldown: time.Minute, IdleFor: 2 * time.Minute, BacklogDelay: 10 * time.Second,
		Freshness: 30 * time.Second, MinHealthySamples: 3, MinDatabaseWrites: 4, MaxDatabaseLatency: 100 * time.Millisecond,
		MaxCPUFraction: .75, MaxIOWaitFraction: .1, WorkerCPUReserve: 1, ReserveMemoryBytes: 1024 << 20, WorkerMemoryBytes: 512 << 20, ReserveDiskBytes: 512 << 20}
}

func (p Policy) Validate() error {
	if p.MaxWorkers < 0 || p.MaxWorkers > 4 || p.Cooldown < 10*time.Second || p.Cooldown > time.Hour || p.IdleFor < p.Cooldown || p.IdleFor > 24*time.Hour || p.BacklogDelay < time.Second || p.BacklogDelay > time.Hour || p.Freshness < time.Second || p.Freshness > 5*time.Minute || p.MinHealthySamples < 2 || p.MinHealthySamples > 60 || p.MinDatabaseWrites < 1 || p.MinDatabaseWrites > 64 || p.MaxDatabaseLatency < time.Millisecond || p.MaxDatabaseLatency > 5*time.Second || math.IsNaN(p.MaxCPUFraction) || p.MaxCPUFraction <= 0 || p.MaxCPUFraction >= 1 || math.IsNaN(p.MaxIOWaitFraction) || p.MaxIOWaitFraction <= 0 || p.MaxIOWaitFraction >= 1 || math.IsNaN(p.WorkerCPUReserve) || p.WorkerCPUReserve <= 0 || p.WorkerCPUReserve > 64 || p.ReserveMemoryBytes < 64<<20 || p.WorkerMemoryBytes < 64<<20 || p.ReserveDiskBytes < 256<<20 {
		return errors.New("invalid local projection worker policy")
	}
	return nil
}

type Resources struct {
	At                   time.Time
	Known                bool
	CPUFraction          float64
	IOWaitFraction       float64
	CPUCapacity          float64
	AvailableMemoryBytes uint64
	MinFreeDiskBytes     uint64
	Reason               string
}

type Observation struct {
	At                        time.Time
	Paused                    bool
	QueueKnown                bool
	Pending                   bool
	OldestAge                 time.Duration
	Resources                 Resources
	DatabaseAt                time.Time
	DatabaseWrites            int
	DatabaseLatency           time.Duration
	DatabaseFailed            bool
	DatabaseCapacityKnown     bool
	DatabaseCapacityAvailable bool
}

type PolicyState struct {
	LastChange     time.Time
	IdleSince      time.Time
	HealthySamples int
}

type Decision struct {
	Desired int
	Reason  string
	State   PolicyState
}

// Decide never launches a process. Unknown or unhealthy signals cannot grant
// capacity. Increases need sustained observations and a deployment-specific
// administrator declaration; that declaration is not an automatic benchmark.
func (p Policy) Decide(s PolicyState, o Observation, active int) (Decision, error) {
	if err := p.Validate(); err != nil {
		return Decision{}, err
	}
	if o.At.IsZero() || active < 0 || active > 4 {
		return Decision{}, errors.New("invalid local projection worker observation")
	}
	d := Decision{Desired: active, Reason: "hold", State: s}
	decrease := func(reason string) (Decision, error) {
		d.Reason, d.State.HealthySamples = reason, 0
		if d.Desired > 0 {
			d.Desired--
			d.State.LastChange = o.At
		}
		return d, nil
	}
	hold := func(reason string) (Decision, error) { d.Reason, d.State.HealthySamples = reason, 0; return d, nil }
	if s.LastChange.After(o.At) {
		return hold("clock_moved_backwards")
	}
	if active > p.MaxWorkers {
		return decrease("configured_limit")
	}
	if p.MaxWorkers == 0 {
		return hold("disabled")
	}
	if o.Paused {
		return decrease("maintenance_or_closed_admission")
	}
	if o.DatabaseFailed {
		return decrease("database_error")
	}
	if !o.DatabaseAt.IsZero() && o.DatabaseLatency >= p.MaxDatabaseLatency {
		return decrease("database_slow_admission_or_completion")
	}
	if o.DatabaseCapacityKnown && !o.DatabaseCapacityAvailable {
		return decrease("database_connection_reserve")
	}
	if !fresh(o.Resources.At, o.At, p.Freshness) || !o.Resources.Known {
		return decrease("resource_observation_unknown")
	}
	r := o.Resources
	if math.IsNaN(r.CPUFraction) || math.IsNaN(r.CPUCapacity) || math.IsNaN(r.IOWaitFraction) || r.CPUFraction < 0 || r.CPUFraction > 1 || r.IOWaitFraction < 0 || r.IOWaitFraction > 1 || r.CPUCapacity <= 0 {
		return decrease("resource_observation_invalid")
	}
	if r.CPUFraction >= p.MaxCPUFraction {
		return decrease("host_or_plugin_CPU_pressure")
	}
	if r.IOWaitFraction >= p.MaxIOWaitFraction {
		return decrease("host_IO_pressure")
	}
	if r.AvailableMemoryBytes < p.ReserveMemoryBytes {
		return decrease("memory_reserve")
	}
	if r.MinFreeDiskBytes < p.ReserveDiskBytes {
		return decrease("disk_reserve")
	}
	if !o.QueueKnown {
		return hold("queue_observation_unknown")
	}
	if !o.Pending {
		d.State.HealthySamples = 0
		if d.State.IdleSince.IsZero() {
			d.State.IdleSince = o.At
		}
		if !d.State.IdleSince.After(o.At) && o.At.Sub(d.State.IdleSince) >= p.IdleFor {
			return decrease("idle_queue")
		}
		return hold("no_ready_backlog")
	}
	d.State.IdleSince = time.Time{}
	if !p.AdministratorConfirmedBenefit {
		return hold("administrator_has_not_confirmed_deployment_benefit")
	}
	if o.OldestAge < p.BacklogDelay {
		return hold("in_process_capacity_sufficient")
	}
	if !fresh(o.DatabaseAt, o.At, p.Freshness) || o.DatabaseWrites < p.MinDatabaseWrites {
		return hold("recent_durable_write_capacity_unknown")
	}
	if !o.DatabaseCapacityKnown {
		return hold("database_connection_budget_unknown")
	}
	if (p.MaxCPUFraction-r.CPUFraction)*r.CPUCapacity < p.WorkerCPUReserve {
		return hold("insufficient_CPU_for_another_worker")
	}
	if r.AvailableMemoryBytes-p.ReserveMemoryBytes < p.WorkerMemoryBytes {
		return hold("insufficient_memory_for_another_worker")
	}
	if active >= p.MaxWorkers {
		return hold("worker_limit")
	}
	d.State.HealthySamples++
	if d.State.HealthySamples < p.MinHealthySamples {
		d.Reason = "waiting_for_sustained_backlog"
		return d, nil
	}
	if !s.LastChange.IsZero() && o.At.Sub(s.LastChange) < p.Cooldown {
		d.Reason = "cooldown"
		return d, nil
	}
	d.Desired++
	d.Reason, d.State.LastChange, d.State.HealthySamples = "healthy_sustained_backlog", o.At, 0
	return d, nil
}

func fresh(sample, now time.Time, age time.Duration) bool {
	return !sample.IsZero() && !sample.After(now) && now.Sub(sample) <= age
}
