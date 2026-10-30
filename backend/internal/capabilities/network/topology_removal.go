package network

import "context"

type TopologyRemovalStore interface {
	RemoveEntry(context.Context, uint, uint) error
}
type TopologyRemoval struct{ Store TopologyRemovalStore }

func (s TopologyRemoval) Entry(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrResourcePermission
	}
	if id == 0 {
		return ErrResourceNotFound
	}
	return s.Store.RemoveEntry(ctx, actor, id)
}

func (s TopologyRemoval) Check(ctx context.Context, actor, id uint) (DeletionCheck, error) {
	if actor == 0 {
		return DeletionCheck{}, ErrResourcePermission
	}
	if store, ok := s.Store.(interface {
		CheckEntryDeletion(context.Context, uint, uint) (DeletionCheck, error)
	}); ok {
		return store.CheckEntryDeletion(ctx, actor, id)
	}
	return DeletionCheck{}, ErrProtocolEndpointRemovalUnavailable
}
