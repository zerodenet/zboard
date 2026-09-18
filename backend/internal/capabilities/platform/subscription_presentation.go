package platform

import "context"

type SubscriptionPresentationRepository interface {
	SubscriptionCamouflage(context.Context) (configuredTarget, siteURL string, err error)
}

type SubscriptionPresentation struct {
	Repository SubscriptionPresentationRepository
}

func (s SubscriptionPresentation) Camouflage(ctx context.Context) (string, string, error) {
	return s.Repository.SubscriptionCamouflage(ctx)
}
