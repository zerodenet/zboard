package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (s *Services) SubscriptionMutations() entitlements.SubscriptionMutations {
	return entitlements.SubscriptionMutations{Repository: subscriptionMutationBridge{extension: entitlementstore.SubscriptionMutations{DB: s.Identity.db, Publish: networkstore.EnqueueSubscriptionPublications}, termination: commercestore.SubscriptionTermination{DB: s.Identity.db}}}
}

// status.cancel retains its existing administrator-only no-refund contract;
// monetary refunds remain a separate explicit commerce command.
type subscriptionMutationBridge struct {
	extension   entitlementstore.SubscriptionMutations
	termination commercestore.SubscriptionTermination
}

func (b subscriptionMutationBridge) Apply(ctx context.Context, actor uint, in entitlements.SubscriptionMutationInput) (entitlements.SubscriptionMutationReceipt, error) {
	if in.Kind != entitlements.SubscriptionCancel {
		return b.extension.Apply(ctx, actor, in)
	}
	out, err := b.termination.CancelEntitlement(ctx, actor, in)
	switch {
	case errors.Is(err, commerce.ErrOrderPermission):
		return entitlements.SubscriptionMutationReceipt{}, entitlements.ErrAdministrativeRead
	case errors.Is(err, commerce.ErrNotFound):
		return entitlements.SubscriptionMutationReceipt{}, entitlements.ErrAccessNotFound
	case errors.Is(err, commerce.ErrSubscriptionValueUnavailable):
		return entitlements.SubscriptionMutationReceipt{}, entitlements.ErrSubscriptionMutationUnavailable
	case errors.Is(err, commerce.ErrSubscriptionSettlementConflict), errors.Is(err, commerce.ErrSubscriptionQuoteChanged):
		return entitlements.SubscriptionMutationReceipt{}, entitlements.ErrSubscriptionMutationConflict
	}
	return out, err
}
