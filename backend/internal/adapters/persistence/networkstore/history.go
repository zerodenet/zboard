package networkstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OperationHistory struct{ DB *gorm.DB }

func (s OperationHistory) PruneOperations(ctx context.Context, kind network.OperationKind, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > network.HistoryBatchSize {
		return 0, errors.New("invalid history batch")
	}
	var value any
	switch kind {
	case network.CertificateOperation:
		value = &model.CertificateOperation{}
	case network.DNSOperation:
		value = &model.ProviderOperation{}
	default:
		return 0, errors.New("invalid operation kind")
	}
	var deleted int64
	err := jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		unresolved := unconfirmedOperationEvidence(tx, kind)
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(value).Where("finished_at < ? AND status IN ?", before, []string{"succeeded", "failed"}).Where("NOT EXISTS (?)", unresolved)
		if kind == network.DNSOperation {
			q = q.Where("resource_type = ?", "dns_record")
		}
		var ids []uint
		if err := q.Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("id IN ?", ids).Delete(value)
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}
