package meteringstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type entitlementQuotaKey struct {
	SubscriptionID uint
	Epoch          uint64
}

func loadClosedEntitlement(tx *gorm.DB, sub model.Subscription, epoch uint64) (model.ClosedEntitlement, error) {
	if epoch >= sub.EntitlementEpoch {
		return model.ClosedEntitlement{}, errors.New("invalid historical entitlement epoch")
	}
	var row model.ClosedEntitlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND epoch = ?", sub.ID, epoch).Take(&row).Error; err != nil {
		return row, err
	}
	if row.UserID != sub.UserID || row.ClosingFlowUsed < 0 || row.FlowUsed < row.ClosingFlowUsed || row.FlowUsed > row.FlowTotal {
		return row, errors.New("invalid historical entitlement quota")
	}
	return row, nil
}
func closedQuotaSubscription(sub model.Subscription, row model.ClosedEntitlement) model.Subscription {
	sub.EntitlementEpoch = row.Epoch
	sub.FlowTotal, sub.FlowUsed, sub.TrafficCalcMode = row.FlowTotal, row.FlowUsed, row.TrafficCalcMode
	return sub
}
func (s *flowBatch) quotaSubscription(tx *gorm.DB, credential zeroFlowCredentialRef, current *model.Subscription) (*model.Subscription, bool, error) {
	if credential.EntitlementEpoch == current.EntitlementEpoch {
		return current, true, nil
	}
	key := entitlementQuotaKey{current.ID, credential.EntitlementEpoch}
	row, ok := s.closed[key]
	if !ok {
		loaded, err := loadClosedEntitlement(tx, *current, credential.EntitlementEpoch)
		if err != nil {
			return nil, false, err
		}
		row = &loaded
		s.closed[key] = row
	}
	copy := closedQuotaSubscription(*current, *row)
	return &copy, false, nil
}
