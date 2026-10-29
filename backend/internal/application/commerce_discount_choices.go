package application

import "github.com/zerodenet/zboard/backend/internal/capabilities/commerce"

func (s *Services) ConfigureCommerceDiscountChoices(provider commerce.DiscountProviderCatalog) {
	s.DiscountProviders = commerce.DiscountProviderChoices{Repository: provider}
}
