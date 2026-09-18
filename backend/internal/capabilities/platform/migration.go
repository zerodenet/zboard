package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrMigrationInvalid          = errors.New("invalid database migration target")
	ErrMigrationConfirmation     = errors.New("confirm must be true after reviewing the migration warning")
	ErrMigrationBusy             = errors.New("source database has unfinished or unverified work")
	ErrMigrationTargetConnection = errors.New("target database connection failed")
	ErrMigrationTargetOccupied   = errors.New("target database is not empty or cannot be inspected")
)

type MigrationTarget struct {
	TargetDriver     string `json:"target_driver"`
	TargetDataSource string `json:"target_datasource"`
}

type MigrationStart struct {
	Target  MigrationTarget
	Confirm bool
}

type MigrationAccepted struct {
	IdempotencyKey        string
	TaskID                uint
	RunID                 string
	Total                 int64
	Status                int16
	Priority, MaxAttempts int
	ScheduledAt           *time.Time
	CreatedAt, UpdatedAt  time.Time
}

type MigrationRepository interface {
	Check(context.Context, uint) error
	// Admit repeats authorization and all admission checks under the shared
	// ledger lock, then commits the task, maintenance gate, Run and audit together.
	Admit(context.Context, uint, string, string) (MigrationAccepted, error)
}
type MigrationTargetChecker interface {
	Check(context.Context, MigrationTarget) error
}
type MigrationCipher interface{ Encrypt(string) (string, error) }

type Migration struct {
	SourceDriver string
	Repository   MigrationRepository
	Targets      MigrationTargetChecker
	Cipher       MigrationCipher
}

func NormalizeMigrationTarget(source string, target MigrationTarget) (MigrationTarget, error) {
	target.TargetDriver = strings.ToLower(strings.TrimSpace(target.TargetDriver))
	target.TargetDataSource = strings.TrimSpace(target.TargetDataSource)
	if (target.TargetDriver != "mysql" && target.TargetDriver != "sqlite") || target.TargetDriver == source || target.TargetDataSource == "" {
		return MigrationTarget{}, ErrMigrationInvalid
	}
	return target, nil
}

func (s Migration) Start(ctx context.Context, actor uint, in MigrationStart) (MigrationAccepted, error) {
	if actor == 0 {
		return MigrationAccepted{}, ErrMaintenancePermission
	}
	target, err := NormalizeMigrationTarget(s.SourceDriver, in.Target)
	if err != nil {
		return MigrationAccepted{}, err
	}
	if !in.Confirm {
		return MigrationAccepted{}, ErrMigrationConfirmation
	}
	// Never connect to a caller-selected target before checking current authority.
	if err := s.Repository.Check(ctx, actor); err != nil {
		return MigrationAccepted{}, err
	}
	if err := s.Targets.Check(ctx, target); err != nil {
		return MigrationAccepted{}, err
	}
	payload, err := json.Marshal(target)
	if err != nil {
		return MigrationAccepted{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(string(payload))
	if err != nil {
		return MigrationAccepted{}, err
	}
	return s.Repository.Admit(ctx, actor, target.TargetDriver, ciphertext)
}
