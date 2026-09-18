package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type protocolEndpointRemovalStoreStub struct {
	actor uint
	id    uint
	now   time.Time
	err   error
}

func (s *protocolEndpointRemovalStoreStub) RemoveProtocolEndpoint(_ context.Context, actor, id uint, now time.Time) (ProtocolEndpointRemovalFacts, error) {
	s.actor, s.id, s.now = actor, id, now
	return ProtocolEndpointRemovalFacts{ID: id, NodeID: 8, Protocol: "vless", WasActive: true}, s.err
}

func TestProtocolEndpointRemovalDelegatesAuthorizedAtomicRemoval(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 9, 10, 0, time.UTC)
	store := &protocolEndpointRemovalStoreStub{}
	service := ProtocolEndpointRemoval{Store: store, Now: func() time.Time { return now }}
	removed, err := service.Remove(context.Background(), 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if store.actor != 7 || store.id != 11 || !store.now.Equal(now) || removed.ID != 11 || !removed.Deleted || removed.RemovedFromRuntime || !removed.RuntimeCleanupQueued {
		t.Fatalf("store=%+v removed=%+v", store, removed)
	}
	if _, err := service.Remove(context.Background(), 0, 11); !errors.Is(err, ErrResourcePermission) {
		t.Fatalf("anonymous removal error=%v", err)
	}
	if _, err := service.Remove(context.Background(), 7, 0); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("zero endpoint error=%v", err)
	}
}
