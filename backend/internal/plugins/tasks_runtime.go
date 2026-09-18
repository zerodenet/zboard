package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

// Plugin lifecycle owns registrations only. Scheduling, capacity and execution
// history belong to the injected host-wide runtime.
type pluginTaskRuntime struct {
	mu         sync.Mutex
	reconcile  sync.Mutex
	pauseCheck func() bool
	runtime    *jobs.Runtime
	registered map[string]string
}

func newPluginTaskRuntime() *pluginTaskRuntime {
	return &pluginTaskRuntime{registered: map[string]string{}}
}
func (m *Manager) SetTaskRuntime(runtime *jobs.Runtime) {
	m.tasks.mu.Lock()
	m.tasks.runtime = runtime
	m.tasks.mu.Unlock()
	m.syncTaskRegistrations(context.Background())
}
func (m *Manager) cancelPluginTasks() {
	m.tasks.mu.Lock()
	runtime := m.tasks.runtime
	ids := m.tasks.registered
	m.tasks.registered = map[string]string{}
	m.tasks.mu.Unlock()
	if runtime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for id := range ids {
			_ = runtime.Deactivate(ctx, id)
		}
	}
}

// deactivatePluginTasks fences one plugin's durable runs before its process is
// stopped or replaced. A persistence failure aborts the lifecycle transition;
// killing the process first would turn an observable cancellation into an
// unclassified crash.
func (m *Manager) deactivatePluginTasks(ctx context.Context, pluginID string) error {
	prefix := "plugin:" + pluginID + ":"
	m.tasks.mu.Lock()
	runtime := m.tasks.runtime
	ids := make([]string, 0)
	for id := range m.tasks.registered {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			ids = append(ids, id)
		}
	}
	m.tasks.mu.Unlock()
	if runtime == nil {
		return nil
	}
	for _, id := range ids {
		if err := runtime.Deactivate(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
func taskUnavailable(task declaredTask) string {
	if !task.owner.Enabled {
		return "disabled"
	}
	if !task.admitted || task.owner.State != "active" {
		return "unavailable"
	}
	return ""
}
func (m *Manager) SetTaskPauseCheck(check func() bool) {
	m.tasks.mu.Lock()
	m.tasks.pauseCheck = check
	m.tasks.mu.Unlock()
}
func (m *Manager) pluginTasksPaused() bool {
	m.tasks.mu.Lock()
	check := m.tasks.pauseCheck
	m.tasks.mu.Unlock()
	return check != nil && check()
}
func (m *Manager) syncTaskRegistrations(parent context.Context) {
	m.tasks.reconcile.Lock()
	defer m.tasks.reconcile.Unlock()
	m.tasks.mu.Lock()
	runtime := m.tasks.runtime
	old := map[string]string{}
	for id, rev := range m.tasks.registered {
		old[id] = rev
	}
	m.tasks.mu.Unlock()
	if runtime == nil {
		return
	}
	if m.lost.Load() {
		m.cancelPluginTasks()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	catalog, err := m.taskCatalog(ctx)
	if err != nil {
		return
	}
	current := map[string]string{}
	for _, task := range catalog {
		if taskUnavailable(task) != "" {
			continue
		}
		id := task.view.ID
		revision := fmt.Sprintf("%s:%d:%d", task.owner.VersionID, task.owner.Generation, task.owner.ConfigRevision)
		if old[id] == revision {
			current[id] = revision
			delete(old, id)
			continue
		}
		if old[id] != "" {
			_ = runtime.Deactivate(ctx, id)
			delete(old, id)
		}
		handler, err := m.TaskExecutor(ctx, task.owner.ID, task.definition.ID)
		if err != nil {
			continue
		}
		err = runtime.Register(jobs.Definition{ID: id, Owner: "plugin:" + task.owner.ID, Name: task.definition.Title, Handler: id, ExecutionGroup: "external", Resource: "plugin:" + task.owner.ID, Revision: revision, Interval: time.Duration(task.definition.IntervalSeconds) * time.Second, Timeout: time.Duration(task.definition.TimeoutSeconds) * time.Second}, handler)
		if err == nil {
			current[id] = revision
		}
	}
	for id := range old {
		_ = runtime.Deactivate(ctx, id)
	}
	m.tasks.mu.Lock()
	m.tasks.registered = current
	m.tasks.mu.Unlock()
}
func (m *Manager) TaskSnapshots(ctx context.Context) ([]TaskView, error) {
	catalog, err := m.taskCatalog(ctx)
	if err != nil {
		return nil, err
	}
	m.tasks.mu.Lock()
	runtime := m.tasks.runtime
	m.tasks.mu.Unlock()
	summaries := map[string]jobs.ScheduleView{}
	if runtime != nil {
		rows, err := runtime.Schedules(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			summaries[row.ID] = row
		}
	}
	out := make([]TaskView, 0, len(catalog))
	for _, task := range catalog {
		view := task.view
		if row, ok := summaries[view.ID]; ok {
			view.Runs = row.Runs
			view.Failures = row.Failures
			view.MissedRuns = row.MissedRuns
			view.TimeZone = row.TimeZone
			view.MisfirePolicy = string(row.MisfirePolicy)
			view.LastStartedAt = row.LastStartedAt
			view.LastFinishedAt = row.LastFinishedAt
			view.LastResult = string(row.LastState)
			if view.LastResult == "" {
				view.LastResult = "never"
			}
			view.NextScanAt = &row.NextAt
			switch row.State {
			case jobs.Running, jobs.CancelRequested:
				view.NextScanAt = nil
				view.Running = 1
				view.State = "running"
			case jobs.Queued:
				view.State = "queued"
			case jobs.Unknown:
				view.NextScanAt = nil
				view.State = "unknown"
			default:
				view.State = "idle"
			}
			if row.LastFinishedAt != nil && row.LastStartedAt != nil {
				view.LastDurationMS = row.LastFinishedAt.Sub(*row.LastStartedAt).Milliseconds()
			}
		}
		if unavailable := taskUnavailable(task); unavailable != "" {
			view.State = unavailable
			view.NextScanAt = nil
		} else if runtime == nil {
			view.State = "unavailable"
		} else if m.pluginTasksPaused() || m.lost.Load() {
			view.State = "paused"
		}
		out = append(out, view)
	}
	return out, nil
}
