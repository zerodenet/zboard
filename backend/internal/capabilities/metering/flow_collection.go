package metering

import (
	"context"
	"strings"
	"time"
)

// FlowStart is normalized by an authenticated event adapter. It carries no
// caller-supplied subscription ownership; the repository resolves that mapping.
type FlowStart struct {
	NodeID                                uint
	CoreInstanceID, EventID, PrincipalKey string
	Sequence                              uint64
	OccurredAt, ReceivedAt                time.Time
}
type FlowCollectionRepository interface {
	Record(context.Context, FlowStart) error
}
type FlowCollection struct{ Repository FlowCollectionRepository }

func (s FlowCollection) Record(ctx context.Context, event FlowStart) error {
	event.EventID = strings.TrimSpace(event.EventID)
	event.CoreInstanceID = strings.TrimSpace(event.CoreInstanceID)
	event.PrincipalKey = strings.TrimSpace(event.PrincipalKey)
	if event.NodeID == 0 || event.EventID == "" {
		return nil
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	event.ReceivedAt = event.ReceivedAt.UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = event.ReceivedAt
	}
	event.OccurredAt = event.OccurredAt.UTC()
	return s.Repository.Record(ctx, event)
}
