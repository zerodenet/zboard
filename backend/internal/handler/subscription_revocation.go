package handler

import (
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

func revokeSubscriptionCredentialsOutsideGroup(tx *gorm.DB, sub model.Subscription, now time.Time) error {
	return application.RevokeSubscriptionOutsideGroup(tx, sub, now)
}
func expireSubscriptionsInTx(tx *gorm.DB, userID uint, now time.Time) error {
	return application.ExpireSubscriptionsInTransaction(tx, userID, now)
}
