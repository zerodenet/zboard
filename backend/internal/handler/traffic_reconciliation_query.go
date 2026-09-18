package handler

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"gorm.io/gorm"
)

func trafficReconciliationTotalsQuery(db, scope *gorm.DB, scoped bool) *gorm.DB {
	return meteringstore.ReconciliationTotalsQuery(db, scope, scoped)
}
