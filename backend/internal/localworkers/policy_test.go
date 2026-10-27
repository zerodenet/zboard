package localworkers

import (
	"testing"
	"time"
)

func healthyCapacity(now time.Time) Observation {
	return Observation{At: now, QueueKnown: true, Pending: true, OldestAge: time.Minute,
		Resources:  Resources{At: now, Known: true, CPUFraction: .1, CPUCapacity: 4, AvailableMemoryBytes: 4 << 30, MinFreeDiskBytes: 4 << 30},
		DatabaseAt: now, DatabaseWrites: 8, DatabaseLatency: time.Millisecond, DatabaseCapacityKnown: true, DatabaseCapacityAvailable: true}
}

func TestLocalWorkerPolicyRequiresAdministratorDeclarationAndRealHeadroom(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name   string
		change func(*Policy, *Observation)
	}{
		{"administrator_not_confirmed", func(p *Policy, _ *Observation) { p.AdministratorConfirmedBenefit = false }},
		{"plugin_CPU_busy", func(_ *Policy, o *Observation) { o.Resources.CPUFraction = .9 }},
		{"database_slow_writes", func(_ *Policy, o *Observation) { o.DatabaseLatency = time.Second }},
		{"database_error", func(_ *Policy, o *Observation) { o.DatabaseFailed = true }},
		{"database_pool_full", func(_ *Policy, o *Observation) { o.DatabaseCapacityAvailable = false }},
		{"database_budget_unknown", func(_ *Policy, o *Observation) { o.DatabaseCapacityKnown = false }},
		{"no_recent_actual_writes", func(_ *Policy, o *Observation) { o.DatabaseWrites = 0 }},
		{"database_stale", func(_ *Policy, o *Observation) { o.DatabaseAt = now.Add(-time.Hour) }},
		{"memory_reserved", func(_ *Policy, o *Observation) { o.Resources.AvailableMemoryBytes = 1 << 30 }},
		{"IO_pressure", func(_ *Policy, o *Observation) { o.Resources.IOWaitFraction = .5 }},
		{"disk_reserved", func(_ *Policy, o *Observation) { o.Resources.MinFreeDiskBytes = 1 }},
		{"resources_unknown", func(_ *Policy, o *Observation) { o.Resources.Known = false }},
		{"resources_stale", func(_ *Policy, o *Observation) { o.Resources.At = now.Add(-time.Hour) }},
		{"CPU_reservation_does_not_fit", func(_ *Policy, o *Observation) { o.Resources.CPUCapacity = .5 }},
		{"queue_unknown", func(_ *Policy, o *Observation) { o.QueueKnown = false }},
		{"no_backlog", func(_ *Policy, o *Observation) { o.Pending = false }},
		{"maintenance", func(_ *Policy, o *Observation) { o.Paused = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := DefaultPolicy()
			p.MaxWorkers, p.AdministratorConfirmedBenefit = 2, true
			o := healthyCapacity(now)
			test.change(&p, &o)
			state := PolicyState{}
			for range 10 {
				d, err := p.Decide(state, o, 0)
				if err != nil || d.Desired != 0 {
					t.Fatal("unhealthy or unconfirmed observation expanded capacity", d, err)
				}
				state = d.State
			}
		})
	}
}

func TestLocalWorkerPolicyCooldownDrainAndIdleHysteresis(t *testing.T) {
	p := DefaultPolicy()
	p.MaxWorkers, p.AdministratorConfirmedBenefit = 2, true
	now := time.Now()
	state := PolicyState{}
	var decision Decision
	for i := 0; i < p.MinHealthySamples; i++ {
		var err error
		decision, err = p.Decide(state, healthyCapacity(now), 0)
		if err != nil || (decision.Desired == 1) != (i == p.MinHealthySamples-1) {
			t.Fatal("sustained backlog requirement", decision, err)
		}
		state = decision.State
	}
	for range 10 {
		decision, _ = p.Decide(state, healthyCapacity(now.Add(time.Second)), 1)
		state = decision.State
		if decision.Desired != 1 {
			t.Fatal("cooldown bypassed", decision)
		}
	}
	o := healthyCapacity(now.Add(2 * time.Second))
	o.DatabaseLatency = time.Second
	decision, _ = p.Decide(state, o, 1)
	if decision.Desired != 0 || decision.Reason != "database_slow_admission_or_completion" {
		t.Fatal("slow database did not reduce optional concurrency", decision)
	}
	o = healthyCapacity(now.Add(time.Hour))
	o.Pending = false
	decision, _ = p.Decide(PolicyState{}, o, 1)
	if decision.Desired != 1 {
		t.Fatal("idle noise immediately removed worker", decision)
	}
	o.At = o.At.Add(p.IdleFor)
	o.Resources.At = o.At
	decision, _ = p.Decide(decision.State, o, 1)
	if decision.Desired != 0 || decision.Reason != "idle_queue" {
		t.Fatal("sustained idle did not drain", decision)
	}
}
