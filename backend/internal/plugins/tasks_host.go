package plugins

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"strings"
	"time"
)

type hostTaskRequest struct {
	Type   string `json:"type"`
	TaskID string `json:"task_id,omitempty"`
	Key    string `json:"key,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}
type hostTaskRun struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	State      jobs.State `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func taskRunView(pluginID string, run jobs.Run) hostTaskRun {
	return hostTaskRun{ID: run.ID, TaskID: strings.TrimPrefix(run.Handler, "plugin:"+pluginID+":"), State: run.State, CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt}
}

// Caller holds the lifecycle lock and has authenticated the exact process.
func (m *Manager) hostTasksLocked(ctx context.Context, id string, request hostTaskRequest) (any, error) {
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	v, err := m.load(id)
	if err != nil {
		return nil, err
	}
	if !v.Enabled || v.State != "active" || !hasCapability(v, TaskCapability) {
		return nil, ErrPermission
	}
	m.tasks.mu.Lock()
	runtime := m.tasks.runtime
	m.tasks.mu.Unlock()
	if runtime == nil {
		return nil, ErrUnavailable
	}
	owner := "plugin:" + id
	switch request.Type {
	case "tasks.submit":
		if !pagePattern.MatchString(request.TaskID) || request.Key == "" || len(request.Key) > 128 {
			return nil, jobs.ErrInvalid
		}
		declared := false
		for _, task := range v.Manifest.Contributions.Tasks {
			if task.ID == request.TaskID {
				declared = true
			}
		}
		if !declared {
			return nil, ErrPermission
		}
		revision := fmt.Sprintf("%s:%d:%d", v.VersionID, v.Generation, v.ConfigRevision)
		run, err := runtime.RequestRegistered(ctx, owner, owner+":"+request.TaskID, revision, request.Key)
		if errors.Is(err, jobs.ErrConflict) {
			return nil, ErrUnavailable
		}
		if err != nil {
			return nil, err
		}
		return taskRunView(id, run), nil
	case "tasks.list":
		limit := request.Limit
		if limit == 0 {
			limit = 20
		}
		if limit < 1 || limit > 100 || request.Offset < 0 {
			return nil, jobs.ErrInvalid
		}
		rows, err := runtime.OwnerRuns(ctx, owner, limit, request.Offset)
		if err != nil {
			return nil, err
		}
		out := make([]hostTaskRun, 0, len(rows))
		for _, run := range rows {
			out = append(out, taskRunView(id, run))
		}
		return map[string]any{"items": out, "limit": limit, "offset": request.Offset}, nil
	default:
		return nil, jobs.ErrInvalid
	}
}
