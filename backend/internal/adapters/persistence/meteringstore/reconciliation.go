package meteringstore

import (
	"context"
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"time"
)

type Reconciliation struct{ DB, ReadDB *gorm.DB }

func (s Reconciliation) Read(ctx context.Context, actor uint, q metering.ReconciliationQuery) (metering.ReconciliationSnapshot, error) {
	var result metering.ReconciliationSnapshot
	err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative)
	if err != nil {
		return metering.ReconciliationSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	load := func(read *gorm.DB) error {
		var err error
		result, err = loadTrafficReconciliation(read, q.UserID, q.SubscriptionID, time.Now().UTC(), q.Paged, q.IssuesOnly, q.Offset, q.Limit)
		return err
	}
	// Release the authority connection before taking a reporting snapshot so
	// a single-connection writer pool remains available for settlement.
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	err = read.WithContext(ctx).Transaction(load, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})

	if err != nil {
		return metering.ReconciliationSnapshot{}, err
	}
	return result, nil
}
