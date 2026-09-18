package entitlements

import (
	"context"
	"errors"
	"time"
)

var ErrCredentialExpiryUnavailable = errors.New("credential expiry unavailable")

type ExpiredCredential struct {
	ID                 uint
	SubscriptionID     uint
	NodeID             uint
	ProtocolEndpointID uint
}

type CredentialExpiryRepository interface {
	ExpireDueCredentials(context.Context, time.Time, int) ([]ExpiredCredential, error)
	HasDueCredentials(context.Context, time.Time) (bool, error)
}

type CredentialExpiry struct{ Repository CredentialExpiryRepository }

func (s CredentialExpiry) ExpireDue(ctx context.Context, now time.Time, limit int) ([]ExpiredCredential, error) {
	if s.Repository == nil {
		return nil, ErrCredentialExpiryUnavailable
	}
	if limit <= 0 {
		limit = 200
	}
	return s.Repository.ExpireDueCredentials(ctx, now.UTC(), limit)
}

func (s CredentialExpiry) HasDue(ctx context.Context, now time.Time) (bool, error) {
	if s.Repository == nil {
		return false, ErrCredentialExpiryUnavailable
	}
	return s.Repository.HasDueCredentials(ctx, now.UTC())
}
