package commerce

import "context"

// Host-only checkout admission; the browser never selects the provider identity.
type PaymentCheckoutRepository interface {
	Open(context.Context, uint, uint, string) (Order, bool, error)
}
type PaymentCheckout struct{ Repository PaymentCheckoutRepository }

func (s PaymentCheckout) Open(ctx context.Context, actor, order uint, provider string) (Order, bool, error) {
	if actor == 0 || order == 0 || provider == "" || len(provider) > 32 {
		return Order{}, false, ErrOrderPermission
	}
	return s.Repository.Open(ctx, actor, order, provider)
}
