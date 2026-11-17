package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// BackfillMeteringReportReceipts is the non-bootstrap application boundary for
// the administrator's bounded local recovery command. No HTTP/plugin admission
// or ability to mutate quota is granted by exposing this Go operation.
func BackfillMeteringReportReceipts(ctx context.Context, db *gorm.DB, after, through uint, limit int) (metering.ReportReceiptBackfillResult, error) {
	if err := datastore.ValidatePreparedReportReceiptSchema(ctx, db); err != nil {
		return metering.ReportReceiptBackfillResult{}, err
	}
	return meteringstore.BackfillReportReceipts(ctx, db, after, through, limit)
}
