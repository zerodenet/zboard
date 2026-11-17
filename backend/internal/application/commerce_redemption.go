package application

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (p CommerceDiscountProvider) RedeemDiscount(ctx context.Context, in commerce.DiscountRedemptionRequest) (commerce.DiscountRedemptionResult, error) {
	provider, ok := p.Native.(commerce.DiscountRedemptionProvider)
	if !ok {
		return commerce.DiscountRedemptionResult{}, commerce.ErrDiscountRedemptionUnsupported
	}
	if err := commerce.ValidateDiscountRedemptionRequest(in); err != nil {
		return commerce.DiscountRedemptionResult{}, err
	}
	out, err := provider.RedeemDiscount(ctx, in)
	if err != nil {
		return out, err
	}
	return out, commerce.ValidateDiscountRedemptionResult(in, out)
}

func (s *Services) DiscountRedemptionJob() PeriodicJob {
	return PeriodicJob{ID: "discount_redemptions", Name: "优惠预留确认与恢复", Interval: 10 * time.Second, Timeout: 2 * time.Minute, External: true, Immediate: true, ReconcileAfterLoss: true, RetryFailures: true, Run: func(ctx context.Context) error {
		provider, _ := s.commerceDiscountQuoter.(commerce.DiscountRedemptionProvider)
		_, err := (commercestore.DiscountRedemptionJobs{DB: s.Identity.db, Provider: provider}).RunBatch(ctx, 10)
		// Domain unknown/failure remains durable; existing periodic scheduling retries
		// the same exact keys. No new business order or financial write is repeated.
		if errors.Is(err, context.Canceled) {
			return ctx.Err()
		}
		return err
	}}
}
