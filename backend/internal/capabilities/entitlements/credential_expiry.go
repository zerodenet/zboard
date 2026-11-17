package entitlements

import (
	"context"
	"errors"
	"time"
)

var ErrCredentialExpiryUnavailable = errors.New("credential expiry unavailable")
var ErrCredentialExpiryScopeBudget = errors.New("credential expiry batch exceeds current order-lock budget")

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

// CredentialAccessExpiryRepository exposes only bounded local revocation and
// publication intent. It does not end financial histories or bypass their locks.
type CredentialAccessExpiryRepository interface {
	ExpireDueAccessCredentials(context.Context, time.Time, int) (int, error)
}

func (s CredentialExpiry) ExpireAccessDue(ctx context.Context, now time.Time, limit int) (int, error) {
	repository, ok := s.Repository.(CredentialAccessExpiryRepository)
	if !ok {
		return 0, ErrCredentialExpiryUnavailable
	}
	if limit <= 0 {
		limit = 200
	}
	return repository.ExpireDueAccessCredentials(ctx, now.UTC(), limit)
}

// A mutable-order budget is distinguishable from a real funding inconsistency.
// The underlying authority error remains visible to existing callers.
type credentialExpiryMutableBudgetError struct{ cause error }

func (e *credentialExpiryMutableBudgetError) Error() string {
	return "credential retirement exceeds mutable-order budget: " + e.cause.Error()
}
func (e *credentialExpiryMutableBudgetError) Unwrap() error { return e.cause }
func NewCredentialExpiryMutableBudgetError(cause error) error {
	if cause == nil {
		return ErrCredentialExpiryUnavailable
	}
	return &credentialExpiryMutableBudgetError{cause: cause}
}

// Only a single, explicit budget failure permits a scheduling deferral. In
// particular a joined budget plus an unrelated database/funding failure is not
// treated as a harmless oversized subscription.
func CredentialExpiryBudgetReason(err error) string {
	for err != nil {
		if err == ErrCredentialExpiryScopeBudget {
			return "order_identity_budget"
		}
		if _, ok := err.(*credentialExpiryMutableBudgetError); ok {
			return "mutable_order_budget"
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return ""
		}
		err = wrapped.Unwrap()
	}
	return ""
}
