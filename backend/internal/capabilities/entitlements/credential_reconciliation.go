package entitlements

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCredentialReconciliationInvalid     = errors.New("credential reconciliation request is invalid")
	ErrCredentialReconciliationUnavailable = errors.New("credential reconciliation capability unavailable")
)

type GroupCredentialReconciliationRepository interface {
	ReconcileGroup(context.Context, uint, time.Time) error
}

type GroupCredentialReconciliation struct {
	Repository GroupCredentialReconciliationRepository
	Now        func() time.Time
}

type NodeCredentialReconciliationRepository interface {
	ReconcileNode(context.Context, uint, time.Time) error
}

// NodeCredentialReconciliation makes the entitlement write that precedes a
// node configuration render explicit. Configuration compilation itself can
// then remain read-only.
type NodeCredentialReconciliation struct {
	Repository NodeCredentialReconciliationRepository
	Now        func() time.Time
}

func (s GroupCredentialReconciliation) ReconcileGroup(ctx context.Context, groupID uint) error {
	if groupID == 0 {
		return ErrCredentialReconciliationInvalid
	}
	if s.Repository == nil {
		return ErrCredentialReconciliationUnavailable
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	return s.Repository.ReconcileGroup(ctx, groupID, now)
}

func (s NodeCredentialReconciliation) ReconcileNode(ctx context.Context, nodeID uint) error {
	if nodeID == 0 {
		return ErrCredentialReconciliationInvalid
	}
	if s.Repository == nil {
		return ErrCredentialReconciliationUnavailable
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	return s.Repository.ReconcileNode(ctx, nodeID, now)
}
