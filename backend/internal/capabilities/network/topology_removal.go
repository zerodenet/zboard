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
