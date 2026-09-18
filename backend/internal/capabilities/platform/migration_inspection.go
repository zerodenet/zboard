package platform

import (
	"context"
	"errors"
	"time"
)

var ErrMigrationSourceSnapshot = errors.New("source database cannot provide a consistent migration lock")

// MigrationTask is an explicit legacy-compatible projection, never a stored
// task payload. Scope and Content remain empty; RunID links shared history.
type MigrationTask struct {
	ID             uint       `json:"id"`
	RunID          string     `json:"run_id,omitempty"`
	Type           string     `json:"type"`
	Scope          string     `json:"scope"`
	Content        string     `json:"content"`
	Status         int16      `json:"status"`
	Errors         string     `json:"errors"`
	Total          int64      `json:"total"`
	Current        int64      `json:"current"`
	IdempotencyKey string     `json:"idempotency_key"`
	Priority       int        `json:"priority"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LockedBy       string     `json:"locked_by"`
	LockedUntil    *time.Time `json:"locked_until"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (a MigrationAccepted) Task() MigrationTask {
	return MigrationTask{ID: a.TaskID, RunID: a.RunID, Type: "database_migration", Status: a.Status, Total: a.Total, IdempotencyKey: a.IdempotencyKey, Priority: a.Priority, MaxAttempts: a.MaxAttempts, ScheduledAt: a.ScheduledAt, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

type MigrationStatus struct {
	SourceDriver string           `json:"source_driver"`
	Task         *MigrationTask   `json:"task,omitempty"`
	Maintenance  MaintenanceState `json:"maintenance"`
	NextStep     string           `json:"next_step,omitempty"`
}
type MigrationStatusData struct {
	Task        *MigrationTask
	Maintenance MaintenanceData
}
type MigrationPreflight struct {
	SourceDriver string `json:"source_driver"`
	TargetDriver string `json:"target_driver"`
	Target       string `json:"target"`
	Tables       int    `json:"tables"`
	Ready        bool   `json:"ready"`
}
type MigrationStatusRepository interface {
	Read(context.Context, uint) (MigrationStatusData, error)
}
type MigrationReadAdmission interface {
	Check(context.Context, uint) error
}
type MigrationSourceProbe interface {
	Snapshot(context.Context) (int, error)
	Describe(MigrationTarget) string
}
type MigrationInspection struct {
	SourceDriver string
	Repository   MigrationStatusRepository
	Admission    MigrationReadAdmission
	Targets      MigrationTargetChecker
	Source       MigrationSourceProbe
}

func (s MigrationInspection) Status(ctx context.Context, actor uint) (MigrationStatus, error) {
	if actor == 0 {
		return MigrationStatus{}, ErrMaintenancePermission
	}
	data, err := s.Repository.Read(ctx, actor)
	if err != nil {
		return MigrationStatus{}, err
	}
	out := MigrationStatus{SourceDriver: s.SourceDriver, Task: data.Task, Maintenance: data.Maintenance.State()}
	if out.Task != nil && out.Task.Status == 2 {
		out.NextStep = "更新 ZBOARD_DATABASE_DRIVER 和 ZBOARD_DATA_SOURCE，重启服务并验证后再关闭维护模式"
	}
	return out, nil
}
func (s MigrationInspection) Preflight(ctx context.Context, actor uint, input MigrationTarget) (MigrationPreflight, error) {
	if actor == 0 {
		return MigrationPreflight{}, ErrMaintenancePermission
	}
	target, err := NormalizeMigrationTarget(s.SourceDriver, input)
	if err != nil {
		return MigrationPreflight{}, err
	}
	if err := s.Admission.Check(ctx, actor); err != nil {
		return MigrationPreflight{}, err
	}
	if err := s.Targets.Check(ctx, target); err != nil {
		return MigrationPreflight{}, err
	}
	if err := s.Admission.Check(ctx, actor); err != nil {
		return MigrationPreflight{}, err
	}
	tables, err := s.Source.Snapshot(ctx)
	if err != nil {
		return MigrationPreflight{}, err
	}
	// Target and source probes may take time. Do not return readiness to an actor
	// revoked during them, or while newly admitted work needs to finish.
	if err := s.Admission.Check(ctx, actor); err != nil {
		return MigrationPreflight{}, err
	}
	return MigrationPreflight{SourceDriver: s.SourceDriver, TargetDriver: target.TargetDriver, Target: s.Source.Describe(target), Tables: tables, Ready: true}, nil
}
