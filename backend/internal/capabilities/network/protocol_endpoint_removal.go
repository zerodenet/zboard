package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProtocolEndpointRemovalUnavailable = errors.New("protocol endpoint removal unavailable")
	ErrProtocolEndpointRemovalConflict    = errors.New("protocol endpoint has a running publication")
	ErrProtocolEndpointResourceDeleting   = errors.New("protocol endpoint node is being deleted")
)

type ProtocolEndpointRemovalFacts struct {
	ID        uint
	NodeID    uint
	Protocol  string
	WasActive bool
}

type ProtocolEndpointRemoved struct {
	ID                   uint `json:"id"`
	Deleted              bool `json:"deleted"`
	RemovedFromRuntime   bool `json:"removed_from_runtime"`
	RuntimeCleanupQueued bool `json:"runtime_cleanup_queued"`
}

type ProtocolEndpointRemovalStore interface {
	RemoveProtocolEndpoint(context.Context, uint, uint, time.Time) (ProtocolEndpointRemovalFacts, error)
}

type ProtocolEndpointRemoval struct {
	Store ProtocolEndpointRemovalStore
	Now   func() time.Time
}

func (s ProtocolEndpointRemoval) Remove(ctx context.Context, actor, id uint) (ProtocolEndpointRemoved, error) {
	if s.Store == nil {
		return ProtocolEndpointRemoved{}, ErrProtocolEndpointRemovalUnavailable
	}
	if actor == 0 {
		return ProtocolEndpointRemoved{}, ErrResourcePermission
	}
	if id == 0 {
		return ProtocolEndpointRemoved{}, ErrResourceNotFound
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	facts, err := s.Store.RemoveProtocolEndpoint(ctx, actor, id, now)
	if err != nil {
		return ProtocolEndpointRemoved{}, err
	}
	return ProtocolEndpointRemoved{
		ID: facts.ID, Deleted: true, RemovedFromRuntime: false, RuntimeCleanupQueued: true,
	}, nil
}
