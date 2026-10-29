package localworkers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type controlledProcess struct {
	done        chan error
	once        sync.Once
	drains      atomic.Int32
	kills       atomic.Int32
	exitOnDrain bool
	exitOnKill  bool
}

func (p *controlledProcess) PID() int           { return 123 }
func (p *controlledProcess) Done() <-chan error { return p.done }
func (p *controlledProcess) exit(err error)     { p.once.Do(func() { p.done <- err; close(p.done) }) }
func (p *controlledProcess) Drain() error {
	p.drains.Add(1)
	if p.exitOnDrain {
		p.exit(nil)
	}
	return nil
}
func (p *controlledProcess) Kill() error {
	p.kills.Add(1)
	if p.exitOnKill {
		p.exit(errors.New("fixture killed"))
	}
	return nil
}

type controlledLauncher struct {
	calls     int
	failure   bool
	processes []*controlledProcess
}

func (l *controlledLauncher) Start(context.Context) (Process, error) {
	l.calls++
	if l.failure {
		return nil, errors.New("fixture fork failure")
	}
	p := &controlledProcess{done: make(chan error, 1)}
	l.processes = append(l.processes, p)
	return p, nil
}

func configuredController() (*Supervisor, *controlledLauncher) {
	p := DefaultPolicy()
	p.MaxWorkers, p.AdministratorConfirmedBenefit, p.MinHealthySamples = 1, true, 2
	p.Cooldown, p.IdleFor = 10*time.Second, 10*time.Second
	l := &controlledLauncher{}
	s := &Supervisor{Policy: p, Interval: time.Second, DrainTimeout: time.Second, Launcher: l}
	return s, l
}

func TestLocalWorkerSupervisorDoesNotReplaceAnUnreapedDrainingProcess(t *testing.T) {
	s, launcher := configuredController()
	now := time.Now()
	s.step(t.Context(), healthyCapacity(now))
	s.step(t.Context(), healthyCapacity(now.Add(time.Second)))
	if launcher.calls != 1 || len(s.children) != 1 {
		t.Fatal("healthy fixture did not launch exactly one process")
	}
	process := launcher.processes[0]
	o := healthyCapacity(now.Add(2 * time.Second))
	o.DatabaseLatency = time.Second
	s.step(t.Context(), o)
	if process.drains.Load() != 1 {
		t.Fatal("slow writes did not request local drain")
	}
	for i := 3; i < 30; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 1 || process.kills.Load() != 1 || len(s.children) != 1 {
		t.Fatal("unconfirmed process exit caused replacement storm", launcher.calls, process.kills.Load())
	}
	process.exit(nil)
	for i := 30; i < 35; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 2 || len(s.children) != 1 {
		t.Fatal("confirmed drain did not make one slot eligible again")
	}
}

func TestLocalWorkerSupervisorLaunchAndCrashUseBoundedBackoff(t *testing.T) {
	s, launcher := configuredController()
	launcher.failure = true
	now := time.Now()
	for i := 0; i < 9; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 1 {
		t.Fatal("failed start caused a launch storm", launcher.calls)
	}
	launcher.failure = false
	for i := 10; i < 15; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 2 {
		t.Fatal("healthy cooldown did not permit one new launch", launcher.calls)
	}
	launcher.processes[0].exit(errors.New("fixture process crash"))
	for i := 15; i < 30; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 2 {
		t.Fatal("crash bypassed increasing restart backoff", launcher.calls)
	}
}

func TestLocalWorkerSupervisorUnconfirmedDeploymentNeverStarts(t *testing.T) {
	s, launcher := configuredController()
	s.Policy.AdministratorConfirmedBenefit = false
	now := time.Now()
	for i := 0; i < 100; i++ {
		s.step(t.Context(), healthyCapacity(now.Add(time.Duration(i)*time.Second)))
	}
	if launcher.calls != 0 {
		t.Fatal("unconfirmed deployment launched a process")
	}
}

type drainingLauncher struct{ created chan *controlledProcess }

func (l drainingLauncher) Start(context.Context) (Process, error) {
	p := &controlledProcess{done: make(chan error, 1), exitOnDrain: true}
	l.created <- p
	return p, nil
}

func TestLocalWorkerSupervisorCancellationDrainsOnlyItsOwnChild(t *testing.T) {
	s, _ := configuredController()
	created := make(chan *controlledProcess, 1)
	s.Launcher, s.Observe = drainingLauncher{created}, func(_ context.Context, now time.Time) Observation { return healthyCapacity(now) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	var child *controlledProcess
	select {
	case child = <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy controlled child did not launch")
	}
	foreign := &controlledProcess{done: make(chan error, 1)}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local child did not drain")
	}
	if child.drains.Load() != 1 || child.kills.Load() != 0 || foreign.drains.Load() != 0 || foreign.kills.Load() != 0 {
		t.Fatal("shutdown escaped its process ownership or killed a graceful child")
	}
}
