package network

import "context"

type ManagedPublicationInventoryRepository interface {
	ListManagedPublicationTargets(context.Context, []string) ([]PublicationRequest, error)
}

type ManagedPublicationInventory struct {
	Repository ManagedPublicationInventoryRepository
}

func (s ManagedPublicationInventory) Targets(ctx context.Context, protocols []string) ([]PublicationRequest, error) {
	if len(protocols) == 0 {
		return []PublicationRequest{}, nil
	}
	return s.Repository.ListManagedPublicationTargets(ctx, protocols)
}
