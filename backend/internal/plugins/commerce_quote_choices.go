package plugins

import (
	"context"
	"errors"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"gorm.io/gorm"
)

// ListCommerceDiscountProviders exposes bounded presentation choices from live
// native installations only. It makes no plugin RPC and never selects a provider
// for the buyer. Offer terms and selection codes remain plugin-owned.
func (m *Manager) ListCommerceDiscountProviders(ctx context.Context) ([]commerce.DiscountProviderChoice, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(m.processes))
	for id := range m.processes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	choices := []commerce.DiscountProviderChoice{}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		installation, process, err := m.commerceQuoteRuntime(ctx, id)
		if errors.Is(err, ErrUnavailable) || errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, ok := process.api.(pluginv1.CommerceQuoteProviderClient); !ok {
			continue
		}
		if len(choices) == 200 {
			return nil, ErrUnavailable // Never silently return a partial catalog.
		}
		choices = append(choices, commerce.DiscountProviderChoice{ProviderID: id, Name: installation.Manifest.Name, Description: installation.Manifest.Description, Operation: "new", AgreementScope: "quote_required"})
	}
	return choices, nil
}
