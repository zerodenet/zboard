package network

import "context"

type PublicationTarget struct {
	NodeID, EndpointID, RequestedBy uint
}

type PublicationTargetRepository interface {
	Resolve(context.Context, Publication) (PublicationTarget, bool, error)
}

type PublicationTargets struct{ Repository PublicationTargetRepository }

func (s PublicationTargets) Resolve(ctx context.Context, item Publication) (PublicationTarget, bool, error) {
	if s.Repository == nil || item.NodeID == 0 {
		return PublicationTarget{}, false, ErrPublicationUnavailable
	}
	return s.Repository.Resolve(ctx, item)
}
