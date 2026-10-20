package commerce

import "context"

// A public choice contains no installation configuration or authority material.
// Availability is revalidated for every quote; discovery cannot authorize funds.
// AgreementScope is quote_required: listing performs no provider evaluation and
// therefore cannot predict a provider's actual initial-only or recurring terms.
type DiscountProviderChoice struct {
	ProviderID     string `json:"provider_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Operation      string `json:"operation"`
	AgreementScope string `json:"agreement_scope"`
}

type DiscountProviderCatalog interface {
	ListCommerceDiscountProviders(context.Context) ([]DiscountProviderChoice, error)
}

type DiscountProviderChoices struct{ Repository DiscountProviderCatalog }

func (s DiscountProviderChoices) List(ctx context.Context, actor uint) ([]DiscountProviderChoice, error) {
	if actor == 0 {
		return nil, ErrOrderPermission
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Repository == nil {
		return []DiscountProviderChoice{}, nil
	}
	return s.Repository.ListCommerceDiscountProviders(ctx)
}
