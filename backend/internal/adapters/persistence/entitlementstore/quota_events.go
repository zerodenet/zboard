package entitlementstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func RecordQuotaEvent(tx *gorm.DB, sub model.Subscription, eventType string, delta, before, after int64, referenceType, referenceID string) error {
	return tx.Create(&model.QuotaEvent{
		SubscriptionID: sub.ID, EventType: eventType, DeltaBytes: delta,
		BalanceBefore: before, BalanceAfter: after,
		ReferenceType: referenceType, ReferenceID: referenceID, Detail: "{}",
	}).Error
}
