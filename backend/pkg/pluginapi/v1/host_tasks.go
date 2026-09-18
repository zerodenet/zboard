package pluginv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HostTasks requests work from ZBoard's shared durable task queue. It does not
// create timers, workers or arbitrary handlers inside the plugin.
type HostTasks struct{ host *HostStorage }

var (
	ErrHostTaskBackpressure = errors.New("host task queue is full")
	ErrHostTaskUnavailable  = errors.New("host task service is temporarily unavailable")
)

type HostTaskRun struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}
type HostTaskPage struct {
	Items  []HostTaskRun `json:"items"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

func HostTasksFromEnvironment() (*HostTasks, error) {
	host, err := HostStorageFromEnvironment()
	if err != nil {
		return nil, err
	}
	return &HostTasks{host: host}, nil
}
func (h *HostTasks) Close() { h.host.Close() }
func (h *HostTasks) Submit(ctx context.Context, taskID, key string) (HostTaskRun, error) {
	var out HostTaskRun
	err := h.call(ctx, map[string]any{"type": "tasks.submit", "task_id": taskID, "key": key}, &out)
	return out, err
}
func (h *HostTasks) List(ctx context.Context, limit, offset int) (HostTaskPage, error) {
	var out HostTaskPage
	err := h.call(ctx, map[string]any{"type": "tasks.list", "limit": limit, "offset": offset}, &out)
	return out, err
}
func (h *HostTasks) call(ctx context.Context, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if len(raw) > 4096 {
		return errors.New("task request too large")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://plugin-host/tasks", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-Plugin-Host", h.host.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := h.host.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return ErrHostTaskBackpressure
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return ErrHostTaskUnavailable
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("host task request denied or unavailable (HTTP %d)", response.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return errors.New("task response too large")
	}
	return json.Unmarshal(raw, out)
}
