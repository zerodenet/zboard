package localworkers

import (
	"context"
	"errors"
	"time"
)

type Process interface {
	PID() int
	Done() <-chan error
	Drain() error
	Kill() error
}

type Launcher interface {
	Start(context.Context) (Process, error)
}

type Status struct {
	At       time.Time
	Running  int
	Draining int
	Desired  int
	Reason   string
}

type child struct {
	process  Process
	started  time.Time
	stopping bool
	deadline time.Time
	killed   bool
}

// Supervisor is owned by exactly one Run goroutine. It supervises at most four
// concrete processes, and never owns queue files, transactions or domain jobs.
type Supervisor struct {
	Policy       Policy
	Interval     time.Duration
	DrainTimeout time.Duration
	Launcher     Launcher
	Observe      func(context.Context, time.Time) Observation
	Report       func(Status)
	children     []*child
	state        PolicyState
	notBefore    time.Time
	failures     int
}

func (s *Supervisor) Validate() error {
	if err := s.Policy.Validate(); err != nil {
		return err
	}
	if s.Interval < time.Second || s.Interval > time.Minute || s.DrainTimeout < time.Second || s.DrainTimeout > time.Minute || s.Launcher == nil || s.Observe == nil {
		return errors.New("invalid local projection supervisor")
	}
	return nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Policy.MaxWorkers == 0 {
		return nil
	}
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return s.shutdown()
		}
		now := time.Now()
		observe, cancel := context.WithTimeout(ctx, min(s.Interval, 2*time.Second))
		observation := s.Observe(observe, now)
		cancel()
		if ctx.Err() == nil {
			s.step(ctx, observation)
		}
		select {
		case <-ctx.Done():
			return s.shutdown()
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) step(ctx context.Context, observation Observation) {
	now := observation.At
	s.reap(now)
	running, draining := s.counts()
	decision, err := s.Policy.Decide(s.state, observation, running)
	if err != nil {
		s.emit(now, running, "invalid_observation")
		return
	}
	s.state = decision.State
	if decision.Desired < running {
		for i := len(s.children) - 1; i >= 0; i-- {
			if !s.children[i].stopping {
				s.stop(s.children[i], now)
				break
			}
		}
	} else if decision.Desired > running {
		if draining > 0 {
			decision.Reason = "waiting_for_local_drain"
		} else if now.Before(s.notBefore) {
			decision.Reason = "worker_restart_backoff"
		} else if running+draining >= s.Policy.MaxWorkers || ctx.Err() != nil {
			decision.Reason = "launch_admission_closed"
		} else {
			process, err := s.Launcher.Start(ctx)
			if err != nil || process == nil {
				s.failed(now)
				decision.Reason = "worker_start_failed"
			} else {
				s.children = append(s.children, &child{process: process, started: now})
				s.state.LastChange = now
			}
		}
	}
	s.emit(now, decision.Desired, decision.Reason)
}

func (s *Supervisor) counts() (running, draining int) {
	for _, child := range s.children {
		if child.stopping {
			draining++
		} else {
			running++
		}
	}
	return
}

func (s *Supervisor) emit(now time.Time, desired int, reason string) {
	if s.Report != nil {
		running, draining := s.counts()
		s.Report(Status{At: now, Running: running, Draining: draining, Desired: desired, Reason: reason})
	}
}

func (s *Supervisor) failed(now time.Time) {
	s.failures = min(s.failures+1, 8)
	backoff := s.Policy.Cooldown * time.Duration(1<<min(s.failures-1, 3))
	backoff = min(backoff, max(5*time.Minute, s.Policy.Cooldown))
	s.notBefore, s.state.LastChange, s.state.HealthySamples = now.Add(backoff), now, 0
}

func (s *Supervisor) stop(child *child, now time.Time) {
	child.stopping, child.deadline = true, now.Add(s.DrainTimeout)
	// Signaling is process-local admission shutdown. Even if delivery fails,
	// retain this slot until Wait confirms exit or the bounded forced stop.
	_ = child.process.Drain()
}

func (s *Supervisor) reap(now time.Time) {
	live := s.children[:0]
	for _, child := range s.children {
		select {
		case <-child.process.Done():
			if !child.stopping {
				if now.Sub(child.started) >= s.Policy.IdleFor {
					s.failures = 0
				}
				s.failed(now)
			}
			continue
		default:
		}
		if child.stopping && !child.killed && !now.Before(child.deadline) {
			_ = child.process.Kill()
			child.killed = true
		}
		live = append(live, child)
	}
	s.children = live
}

func (s *Supervisor) shutdown() error {
	now := time.Now()
	for _, child := range s.children {
		if !child.stopping {
			s.stop(child, now)
		}
	}
	// All children are asked to drain together. Expiry interrupts only these
	// processes; the durable store retains foreign/unknown intents for recovery.
	deadline := time.NewTimer(s.DrainTimeout + 2*time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		s.reap(time.Now())
		if len(s.children) == 0 {
			s.emit(time.Now(), 0, "drained")
			return nil
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			s.emit(time.Now(), 0, "worker_exit_not_confirmed")
			return errors.New("local projection worker exit not confirmed within shutdown budget")
		}
	}
}
