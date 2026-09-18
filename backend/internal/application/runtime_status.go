package application

import (
	"context"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/jobrun"
)

type ExecutionQueueStatus struct {
	PendingLimit            int   `json:"pending_limit"`
	PluginPendingLimit      int   `json:"plugin_pending_limit"`
	PluginOwnerPendingLimit int   `json:"plugin_owner_pending_limit"`
	MaintenanceReserved     bool  `json:"maintenance_reserved"`
	Pending                 int64 `json:"pending"`
	Running                 int64 `json:"running"`
	Delayed                 int64 `json:"delayed"`
	Unknown                 int64 `json:"unknown"`
}

type RuntimeExecutionStatus struct {
	Jobs             []jobrun.Snapshot
	Capacity         int
	ExternalCapacity int
	Queue            ExecutionQueueStatus
}

func (s *Services) RuntimeExecutionStatus(ctx context.Context) (RuntimeExecutionStatus, error) {
	rows, err := s.Jobs.Schedules(ctx)
	if err != nil {
		return RuntimeExecutionStatus{}, err
	}
	var budget jobstore.Budget
	if err := s.Identity.db.WithContext(ctx).First(&budget, 1).Error; err != nil {
		return RuntimeExecutionStatus{}, err
	}
	var external jobstore.ExecutionGroup
	if err := s.Identity.db.WithContext(ctx).First(&external, "id = ?", "external").Error; err != nil {
		return RuntimeExecutionStatus{}, err
	}
	queue, err := jobstore.New(s.Identity.db).QueueStatus(ctx)
	if err != nil {
		return RuntimeExecutionStatus{}, err
	}
	periodic := runtimeScheduleSnapshots(rows)
	native, err := s.nativeRuntimeSnapshots(ctx)
	if err != nil {
		return RuntimeExecutionStatus{}, err
	}
	return RuntimeExecutionStatus{
		Jobs: append(periodic, native...), Capacity: budget.Capacity, ExternalCapacity: external.Capacity,
		Queue: ExecutionQueueStatus{PendingLimit: queue.PendingLimit, PluginPendingLimit: queue.PluginPendingLimit, PluginOwnerPendingLimit: queue.PluginOwnerPendingLimit, MaintenanceReserved: queue.MaintenanceReserved, Pending: queue.Pending, Running: queue.Running, Delayed: queue.Delayed, Unknown: queue.Unknown},
	}, nil
}

func runtimeScheduleSnapshots(rows []jobs.ScheduleView) []jobrun.Snapshot {
	out := make([]jobrun.Snapshot, 0, len(rows))
	for _, row := range rows {
		if row.Owner != "system" {
			continue
		}
		view := jobrun.Snapshot{ID: row.ID, Name: row.Name, Kind: "periodic", IntervalSeconds: row.Interval.Seconds(), Concurrency: 1, Runs: row.Runs, Failures: row.Failures, MissedRuns: row.MissedRuns, TimeZone: row.TimeZone, MisfirePolicy: string(row.MisfirePolicy), State: "idle", LastResult: string(row.LastState), LastStartedAt: row.LastStartedAt, LastFinishedAt: row.LastFinishedAt}
		if view.LastResult == "" {
			view.LastResult = "never"
		}
		switch row.State {
		case jobs.Running, jobs.CancelRequested:
			view.State, view.Running = "running", 1
		case jobs.Queued:
			view.State = "queued"
		case jobs.Unknown:
			view.State = "unknown"
		}
		if row.ID == "admin_tasks" || row.ID == "event_consumer" || strings.HasPrefix(row.ID, "node_publish_") {
			view.Kind = "queue"
		}
		if view.State == "idle" && view.Kind != "queue" {
			next := row.NextAt
			view.NextScanAt = &next
		}
		if row.LastFinishedAt != nil && row.LastStartedAt != nil && !row.LastFinishedAt.Before(*row.LastStartedAt) {
			view.LastDurationMS = row.LastFinishedAt.Sub(*row.LastStartedAt).Milliseconds()
		}
		out = append(out, view)
	}
	merged := make([]jobrun.Snapshot, 0, len(out))
	var publication *jobrun.Snapshot
	for _, view := range out {
		if !strings.HasPrefix(view.ID, "node_publish_") {
			merged = append(merged, view)
			continue
		}
		if publication == nil {
			copy := view
			copy.ID, copy.Name = "node_publish", "节点配置发布"
			publication = &copy
			continue
		}
		publication.Running += view.Running
		publication.Runs += view.Runs
		publication.Failures += view.Failures
		publication.MissedRuns += view.MissedRuns
		publication.Concurrency += view.Concurrency
		if view.LastFinishedAt != nil && (publication.LastFinishedAt == nil || view.LastFinishedAt.After(*publication.LastFinishedAt)) {
			publication.LastFinishedAt, publication.LastStartedAt = view.LastFinishedAt, view.LastStartedAt
			publication.LastDurationMS, publication.LastResult = view.LastDurationMS, view.LastResult
		}
		if view.State == "unknown" {
			publication.State = "unknown"
		} else if publication.State != "unknown" && (view.State == "running" || view.State == "queued") {
			publication.State = view.State
		}
	}
	if publication != nil {
		if publication.Running > 0 {
			publication.State = "running"
		}
		merged = append(merged, *publication)
	}
	return merged
}

func (s *Services) nativeRuntimeSnapshots(ctx context.Context) ([]jobrun.Snapshot, error) {
	definitions := []struct{ id, name string }{{"certificate_operation", "证书签发"}, {"dns_operation", "DNS 同步"}, {"dns_reconcile", "DNS 结果核验"}, {"database_migration", "数据库迁移"}}
	handlers := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		handlers = append(handlers, definition.id)
	}
	rows, err := jobstore.New(s.Identity.db).NativeExecutionStatuses(ctx, handlers)
	if err != nil {
		return nil, err
	}
	byHandler := make(map[string]jobstore.NativeExecutionStatus, len(rows))
	for _, row := range rows {
		byHandler[row.Handler] = row
	}
	out := make([]jobrun.Snapshot, 0, len(definitions))
	for _, definition := range definitions {
		status := byHandler[definition.id]
		view := jobrun.Snapshot{ID: definition.id, Name: definition.name, Kind: "on_demand", State: "idle", LastResult: "never", Runs: status.Runs, Failures: status.Failures, Running: status.Running}
		if status.Running > 0 {
			view.State = "running"
		} else if status.Unknown > 0 {
			view.State = "unknown"
		} else if status.Pending > 0 {
			view.State = "queued"
		}
		if status.LastFinishedAt != nil {
			view.LastResult, view.LastFinishedAt = status.LastState, status.LastFinishedAt
		}
		out = append(out, view)
	}
	return out, nil
}
