package platform

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrMaintenancePermission = errors.New("maintenance requires current administrator")
	ErrMaintenanceInvalid    = errors.New("maintenance title and message are required and exceed their allowed size")
	ErrMaintenanceRevision   = errors.New("system config revision conflict")
	ErrMaintenanceIncomplete = errors.New("maintenance settings are incomplete")
	ErrMaintenanceBusy       = errors.New("database migration execution is unresolved; maintenance mode cannot be disabled")
)

type MaintenanceState struct {
	Enabled                 bool   `json:"enabled"`
	Title                   string `json:"title"`
	Message                 string `json:"message"`
	MigrationInProgress     bool   `json:"migration_in_progress"`
	MigrationCutoverPending bool   `json:"migration_cutover_pending"`
}

type MaintenanceUpdate struct {
	Enabled           bool              `json:"enabled"`
	Title             string            `json:"title"`
	Message           string            `json:"message"`
	ExpectedRevisions map[string]uint64 `json:"expected_revisions"`
}

// MaintenanceData is a consistent view of platform settings and migration facts.
// It carries no database models or decrypted migration credentials.
type MaintenanceData struct {
	Enabled            bool
	Title, Message     string
	MigrationCompleted bool
	MigrationActive    bool
	Revisions          map[string]uint64
}

func (d MaintenanceData) State() MaintenanceState {
	s := MaintenanceState{Enabled: d.Enabled || d.MigrationActive, Title: "系统维护中", Message: "系统正在维护，请稍后再试。", MigrationInProgress: d.MigrationActive, MigrationCutoverPending: d.Enabled && d.MigrationCompleted}
	if value := strings.TrimSpace(d.Title); value != "" {
		s.Title = value
	}
	if value := strings.TrimSpace(d.Message); value != "" {
		s.Message = value
	}
	return s
}

// ValidateMaintenanceTransition runs inside the repository's admission lock.
func ValidateMaintenanceTransition(current MaintenanceData, in MaintenanceUpdate) error {
	if !in.Enabled && current.MigrationActive {
		return ErrMaintenanceBusy
	}
	for _, key := range []string{"maintenance_enabled", "maintenance_title", "maintenance_message"} {
		revision, exists := current.Revisions[key]
		if !exists {
			return ErrMaintenanceIncomplete
		}
		expected, supplied := in.ExpectedRevisions[key]
		if !supplied || expected != revision {
			return ErrMaintenanceRevision
		}
	}
	return nil
}

type MaintenanceRepository interface {
	Read(context.Context) (MaintenanceData, error)
	// Apply rechecks the current administrator and transition under the common
	// jobs ledger lock, committing settings, revisions and audit together.
	Apply(context.Context, uint, MaintenanceUpdate) error
	Patch(context.Context, uint, MaintenanceSetting) error
}

type Maintenance struct{ Repository MaintenanceRepository }

// State is the public, non-sensitive maintenance projection.
func (s Maintenance) State(ctx context.Context) (MaintenanceState, error) {
	data, err := s.Repository.Read(ctx)
	if err != nil {
		return MaintenanceState{}, err
	}
	return data.State(), nil
}
func (s Maintenance) Update(ctx context.Context, actor uint, in MaintenanceUpdate) error {
	if actor == 0 {
		return ErrMaintenancePermission
	}
	in.Title, in.Message = strings.TrimSpace(in.Title), strings.TrimSpace(in.Message)
	if in.Title == "" || len(in.Title) > 160 || in.Message == "" || len(in.Message) > 4000 {
		return ErrMaintenanceInvalid
	}
	return s.Repository.Apply(ctx, actor, in)
}

// MaintenanceSetting retains the single-setting API's optional revision contract.
// The repository still locks and validates the entire maintenance transition.
type MaintenanceSetting struct {
	Key              string
	Value            string
	ExpectedRevision *uint64
}

func (s Maintenance) Patch(ctx context.Context, actor uint, in MaintenanceSetting) error {
	if actor == 0 {
		return ErrMaintenancePermission
	}
	in.Value = strings.TrimSpace(in.Value)
	switch in.Key {
	case "maintenance_enabled":
		if in.Value != "true" && in.Value != "false" {
			return ErrMaintenanceInvalid
		}
	case "maintenance_title":
		if in.Value == "" || len(in.Value) > 160 {
			return ErrMaintenanceInvalid
		}
	case "maintenance_message":
		if in.Value == "" || len(in.Value) > 4000 {
			return ErrMaintenanceInvalid
		}
	default:
		return ErrMaintenanceInvalid
	}
	return s.Repository.Patch(ctx, actor, in)
}

// ValidateMaintenanceSetting applies the same exclusion policy to the legacy
// one-key command while allowing explanation changes during maintenance.
func ValidateMaintenanceSetting(current MaintenanceData, in MaintenanceSetting) error {
	revision, exists := current.Revisions[in.Key]
	if !exists {
		return ErrMaintenanceIncomplete
	}
	if in.ExpectedRevision != nil && *in.ExpectedRevision != revision {
		return ErrMaintenanceRevision
	}
	enabled := current.State().Enabled
	if in.Key == "maintenance_enabled" {
		enabled = in.Value == "true"
	}
	return ValidateMaintenanceTransition(current, MaintenanceUpdate{Enabled: enabled, ExpectedRevisions: current.Revisions})
}
