package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (s *Services) SubscriptionMutations() entitlements.SubscriptionMutations {
	return entitlements.SubscriptionMutations{Repository: entitlementstore.SubscriptionMutations{DB: s.Identity.db, Publish: networkstore.EnqueueSubscriptionPublications}}
}
