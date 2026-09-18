package entitlements

import (
	"context"
	"errors"
	"testing"
	"time"
)

type groupCredentialRepositoryStub struct {
	groupID uint
	now     time.Time
	err     error
}

type nodeCredentialRepositoryStub struct {
	nodeID uint
	now    time.Time
	err    error
}

func (s *nodeCredentialRepositoryStub) ReconcileNode(_ context.Context, nodeID uint, now time.Time) error {
	s.nodeID, s.now = nodeID, now
	return s.err
}

func (s *groupCredentialRepositoryStub) ReconcileGroup(_ context.Context, groupID uint, now time.Time) error {
	s.groupID, s.now = groupID, now
	return s.err
}

func TestGroupCredentialReconciliationValidatesAndDelegates(t *testing.T) {
	wantTime := time.Date(2026, time.September, 16, 8, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repository := &groupCredentialRepositoryStub{}
	service := GroupCredentialReconciliation{Repository: repository, Now: func() time.Time { return wantTime }}
	if err := service.ReconcileGroup(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if repository.groupID != 7 || !repository.now.Equal(wantTime) || repository.now.Location() != time.UTC {
		t.Fatalf("delegation mismatch: group=%d now=%v", repository.groupID, repository.now)
	}
	if err := service.ReconcileGroup(context.Background(), 0); !errors.Is(err, ErrCredentialReconciliationInvalid) {
		t.Fatalf("invalid group error = %v", err)
	}
	if err := (GroupCredentialReconciliation{}).ReconcileGroup(context.Background(), 7); !errors.Is(err, ErrCredentialReconciliationUnavailable) {
		t.Fatalf("unavailable error = %v", err)
	}
}

func TestNodeCredentialReconciliationValidatesAndDelegates(t *testing.T) {
	wantTime := time.Date(2026, time.September, 16, 9, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repository := &nodeCredentialRepositoryStub{}
	service := NodeCredentialReconciliation{Repository: repository, Now: func() time.Time { return wantTime }}
	if err := service.ReconcileNode(context.Background(), 11); err != nil {
		t.Fatal(err)
	}
	if repository.nodeID != 11 || !repository.now.Equal(wantTime) || repository.now.Location() != time.UTC {
		t.Fatalf("delegation mismatch: node=%d now=%v", repository.nodeID, repository.now)
	}
	if err := service.ReconcileNode(context.Background(), 0); !errors.Is(err, ErrCredentialReconciliationInvalid) {
		t.Fatalf("invalid node error = %v", err)
	}
	if err := (NodeCredentialReconciliation{}).ReconcileNode(context.Background(), 11); !errors.Is(err, ErrCredentialReconciliationUnavailable) {
		t.Fatalf("unavailable error = %v", err)
	}
}
