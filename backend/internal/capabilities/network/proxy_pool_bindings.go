package network

import "context"

type ProxyPoolBindingRepository interface {
	SetProxyPoolNodeReference(context.Context, uint, uint, uint, bool) error
}

func (s ProxyPoolMutations) SetNodeReference(ctx context.Context, actor, pool, node uint, attach bool) error {
	repository, ok := s.Repository.(ProxyPoolBindingRepository)
	if !ok || actor == 0 || pool == 0 || node == 0 {
		return ErrProxyPoolMutationUnavailable
	}
	return repository.SetProxyPoolNodeReference(ctx, actor, pool, node, attach)
}
