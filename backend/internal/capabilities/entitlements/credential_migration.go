package entitlements

import (
	"context"
	"time"
)

type CredentialMigrationStatus struct {
	SubscriptionID        uint       `json:"subscription_id"`
	State                 string     `json:"state"`
	LegacyCredentials     int64      `json:"legacy_credentials"`
	PendingConfirmation   int64      `json:"pending_confirmation"`
	CompatibleCredentials int64      `json:"compatible_credentials"`
	RevokingCredentials   int64      `json:"revoking_credentials"`
	RetiredCredentials    int64      `json:"retired_credentials"`
	CompatibilityUntil    *time.Time `json:"compatibility_until,omitempty"`
	CanRollback           bool       `json:"can_rollback"`
}

type CredentialMigrationRepository interface {
	Status(context.Context, uint, uint) (CredentialMigrationStatus, error)
	Prepare(context.Context, uint, uint) error
	Rollback(context.Context, uint, uint) error
	ProcessCompatibility(context.Context, time.Time, int) (int, error)
}

type CredentialMigration struct{ Repository CredentialMigrationRepository }

func (s CredentialMigration) Status(ctx context.Context, actor, id uint) (CredentialMigrationStatus, error) {
	return s.Repository.Status(ctx, actor, id)
}
func (s CredentialMigration) Prepare(ctx context.Context, actor, id uint) error {
	return s.Repository.Prepare(ctx, actor, id)
}
func (s CredentialMigration) Rollback(ctx context.Context, actor, id uint) error {
	return s.Repository.Rollback(ctx, actor, id)
}
func (s CredentialMigration) ProcessCompatibility(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.Repository.ProcessCompatibility(ctx, now, limit)
}
