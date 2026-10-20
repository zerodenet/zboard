package application

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

type NativeCommerceDiscountProvider interface {
	EvaluateCommerceDiscount(context.Context, string, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error)
}

// CommerceDiscountProvider adapts only the native quote contract. No payment,
// order, balance or entitlement mutation is reachable through this adapter.
type CommerceDiscountProvider struct {
	Native NativeCommerceDiscountProvider
}

func (p CommerceDiscountProvider) Quote(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	if err := ctx.Err(); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if err := commerce.ValidateDiscountQuoteContext(in); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if p.Native == nil || provider == "" {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteUnavailable
	}
	out, err := p.Native.EvaluateCommerceDiscount(ctx, provider, in)
	if err != nil {
		if ctx.Err() != nil {
			return commerce.DiscountQuote{}, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return commerce.DiscountQuote{}, errors.Join(commerce.ErrDiscountQuoteUnavailable, err)
		}
		if errors.Is(err, plugins.ErrUnavailable) || errors.Is(err, plugins.ErrConflict) || errors.Is(err, plugins.ErrRateLimited) {
			return commerce.DiscountQuote{}, errors.Join(commerce.ErrDiscountQuoteUnavailable, err)
		}
		return commerce.DiscountQuote{}, err
	}
	if out.Provider.PluginID != provider || commerce.ValidateDiscountQuote(in, out, time.Now().UTC()) != nil {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteInvalid
	}
	return out, nil
}

var _ commerce.DiscountQuoter = CommerceDiscountProvider{}

// ConfigureCommerceDiscountProvider is composition-only; an absent native host
// leaves explicit discount requests unavailable while ordinary orders proceed.
// Call during startup before serving requests.
func (s *Services) ConfigureCommerceDiscountProvider(native NativeCommerceDiscountProvider) {
	var quoter commerce.DiscountQuoter
	if native != nil {
		quoter = CommerceDiscountProvider{Native: native}
	}
	s.commerceDiscountQuoter = quoter
	s.OrderCreation = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: s.Identity.db, Discounts: quoter}}
}
