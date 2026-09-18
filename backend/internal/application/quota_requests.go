package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (s *Services) QuotaRequests() entitlements.QuotaRequests {
	return entitlements.QuotaRequests{Repository: entitlementstore.QuotaRequests{DB: s.Identity.db}}
}

func (s *Services) QuotaExecution() entitlements.QuotaExecution {
	return entitlements.QuotaExecution{Repository: entitlementstore.QuotaExecution{DB: s.Identity.db, Publish: networkstore.EnqueueSubscriptionPublications}}
}
