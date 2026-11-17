package networkstore

import (
	"context"
	"errors"
	"time"

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
	query, value, err := operationHistoryQuery(s.DB.WithContext(ctx), kind, before)
	if err != nil {
		return 0, err
	}
	var candidates []uint
	if err := query.Order("finished_at,id").Limit(limit).Pluck("id", &candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	var deleted int64
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		locked, _, err := operationHistoryQuery(tx, kind, before)
		if err != nil {
			return err
		}
		if err := locked.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", candidates).Order("id").Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		res := tx.Where("id IN ?", ids).Delete(value)
		deleted = res.RowsAffected
		return res.Error
	})
	return deleted, err
}

func operationHistoryQuery(db *gorm.DB, kind network.OperationKind, before time.Time) (*gorm.DB, any, error) {
	var value any
	switch kind {
	case network.CertificateOperation:
		value = &model.CertificateOperation{}
	case network.DNSOperation:
		value = &model.ProviderOperation{}
	default:
		return nil, nil, errors.New("invalid operation kind")
	}
	q := db.Model(value).Where("finished_at < ? AND status IN ?", before, []string{"succeeded", "failed"}).Where("NOT EXISTS (?)", unconfirmedOperationEvidence(db, kind))
	if kind == network.DNSOperation {
		q = q.Where("resource_type = ?", "dns_record")
	}
	return q, value, nil
}
func (s OperationHistory) HasPendingOperations(ctx context.Context, kind network.OperationKind, before time.Time) (bool, error) {
	q, _, err := operationHistoryQuery(s.DB.WithContext(ctx), kind, before)
	if err != nil {
		return false, err
	}
	var ids []uint
	err = q.Order("finished_at,id").Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
}
