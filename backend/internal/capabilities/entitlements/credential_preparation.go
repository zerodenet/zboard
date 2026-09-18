package entitlements

import (
	"context"
	"time"
)

type CredentialSubscription struct {
	ID, UserID, NodeGroupID uint
	EndAt                   time.Time
}

type CredentialPreparationRepository interface {
	EnsureCredentialSubscriptions(context.Context, []CredentialSubscription) error
	ListActiveMieruCredentialSubscriptions(context.Context, time.Time) ([]CredentialSubscription, error)
}

type CredentialPreparation struct {
	Repository CredentialPreparationRepository
}

func (s CredentialPreparation) Ensure(ctx context.Context, subscriptions []CredentialSubscription) error {
	if len(subscriptions) == 0 {
		return nil
	}
	return s.Repository.EnsureCredentialSubscriptions(ctx, subscriptions)
}

func (s CredentialPreparation) ActiveMieru(ctx context.Context, now time.Time) ([]CredentialSubscription, error) {
	return s.Repository.ListActiveMieruCredentialSubscriptions(ctx, now)
}
