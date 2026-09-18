package plugins

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

// TaskExecutor adapts a particular admitted installation generation to the
// common jobs contract. It owns neither scheduling nor execution capacity.
// A new handler must be constructed after replacement or reconfiguration.
func (m *Manager) TaskExecutor(ctx context.Context, pluginID, taskID string) (jobs.Handler, error) {
	catalog, err := m.taskCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var declaration *declaredTask
	for _, task := range catalog {
		if task.owner.ID == pluginID && task.definition.ID == taskID {
			declaration = &task
			break
		}
	}
	if declaration == nil || taskUnavailable(*declaration) != "" {
		return nil, ErrUnavailable
	}
	before, err := m.taskExecutionSnapshot(ctx, *declaration)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, run jobs.Run) error {
		if run.Owner != "plugin:"+pluginID || run.Handler != declaration.view.ID {
			return ErrPermission
		}
		if m.pluginTasksPaused() {
			return ErrUnavailable
		}
		m.mu.Lock()
		err := m.checkRuntimeSnapshot(before)
		p := m.processes[pluginID]
		if err == nil && (p == nil || p.client.Exited()) {
			err = ErrUnavailable
		}
		m.mu.Unlock()
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(declaration.definition.TimeoutSeconds)*time.Second)
		defer cancel()
		result, err := p.api.RunTask(callCtx, &pluginv1.TaskRunRequest{TaskId: taskID, RunId: run.ID, Generation: before.Generation, ConfigRevision: before.ConfigRevision})
		if callCtx.Err() != nil {
			return callCtx.Err()
		}
		if err != nil || result == nil {
			return jobs.ErrUncertain
		}
		if !result.Succeeded {
			return errors.New("plugin task failed")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.checkRuntimeSnapshot(before); err != nil {
			return errors.Join(jobs.ErrUncertain, err)
		}
		return nil
	}, nil
}

func (m *Manager) taskExecutionSnapshot(ctx context.Context, task declaredTask) (Installation, error) {
	if err := ctx.Err(); err != nil {
		return Installation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return Installation{}, err
	}
	current, err := m.load(task.owner.ID)
	if err != nil {
		return Installation{}, err
	}
	if current.Generation != task.owner.Generation || current.ConfigRevision != task.owner.ConfigRevision || current.VersionID != task.owner.VersionID || !current.Enabled || current.State != "active" {
		return Installation{}, ErrConflict
	}
	return current, nil
}
