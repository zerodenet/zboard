package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

func (s *Services) NodeReports() metering.NodeReports {
	return metering.NodeReports{Repository: meteringstore.NodeReports{DB: s.Identity.db, Expire: entitlementstore.ExpireInTransaction, QuotaEvent: entitlementstore.RecordQuotaEvent}}
}
