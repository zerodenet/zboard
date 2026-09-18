package entitlementstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureAccessToken(db *gorm.DB, subscription model.Subscription, cipher entitlements.AccessCipher) (model.SubscriptionToken, string, error) {
	var existing model.SubscriptionToken
	err := db.Where("subscription_id = ? AND user_id = ?", subscription.ID, subscription.UserID).First(&existing).Error
	if err == nil {
		return existing, "", nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SubscriptionToken{}, "", err
	}

	rawToken, tokenHash, prefix, err := entitlements.NewAccessToken()
	if err != nil {
		return model.SubscriptionToken{}, "", err
	}
	encryptedToken, err := cipher.Encrypt(rawToken)
	if err != nil {
		return model.SubscriptionToken{}, "", err
	}
	subscriptionID := subscription.ID
	candidate := model.SubscriptionToken{
		UserID:          subscription.UserID,
		SubscriptionID:  &subscriptionID,
		TokenHash:       tokenHash,
		TokenCiphertext: encryptedToken,
		TokenPrefix:     prefix,
	}
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subscription_id"}},
		DoNothing: true,
	}).Create(&candidate)
	if result.Error != nil {
		return model.SubscriptionToken{}, "", result.Error
	}
	if result.RowsAffected > 0 {
		return candidate, rawToken, nil
	}
	if err := db.Where("subscription_id = ? AND user_id = ?", subscription.ID, subscription.UserID).First(&existing).Error; err != nil {
		return model.SubscriptionToken{}, "", err
	}
	return existing, "", nil
}
