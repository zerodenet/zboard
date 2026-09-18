package metering

import (
	"context"
	"strings"
	"time"
)

type PrincipalEvent struct {
	CoreInstanceID, EventID string
	Sequence                uint64
	ObservedAt              time.Time
}
type PrincipalObservation struct {
	PrincipalKey            string
	ActiveFlows             uint64
	SessionRegistryRevision uint64
	ObservedAt              time.Time
}
type PrincipalCollectionRepository interface {
	Boundary(context.Context, uint, PrincipalEvent, bool) error
	Observe(context.Context, uint, PrincipalEvent, PrincipalObservation) error
}
type PrincipalCollection struct{ Repository PrincipalCollectionRepository }

func (s PrincipalCollection) Boundary(ctx context.Context, node uint, event PrincipalEvent, stopped bool) error {
	event.CoreInstanceID = strings.TrimSpace(event.CoreInstanceID)
	if node == 0 || event.CoreInstanceID == "" {
		return nil
	}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}
	return s.Repository.Boundary(ctx, node, event, stopped)
}
func (s PrincipalCollection) Observe(ctx context.Context, node uint, event PrincipalEvent, observation PrincipalObservation) error {
	observation.PrincipalKey = strings.TrimSpace(observation.PrincipalKey)
	if node == 0 || observation.PrincipalKey == "" {
		return nil
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	return s.Repository.Observe(ctx, node, event, observation)
}

// GenerationAcceptsObservation prevents a stopped or superseded instance from
// restoring current activity, while historical facts may still be retained.
func GenerationAcceptsObservation(currentInstance string, startedAt time.Time, closedAt *time.Time, incoming string, observedAt time.Time) bool {
	if currentInstance == incoming {
		return closedAt == nil
	}
	return startedAt.IsZero() || !observedAt.Before(startedAt)
}
func ShouldReplaceGeneration(currentInstance string, startedAt time.Time, incoming string, observedAt time.Time) bool {
	return currentInstance != incoming && (startedAt.IsZero() || !observedAt.Before(startedAt))
}
func SupersedesPrincipalSnapshot(currentInstance string, currentRevision uint64, incoming string, revision uint64) bool {
	return currentInstance != incoming || revision > currentRevision
}
