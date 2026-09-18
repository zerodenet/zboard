package entitlementstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CredentialExpiry struct {
	DB      *gorm.DB
	Publish func(*gorm.DB, uint, uint, uint) error
}

func (s CredentialExpiry) ExpireDueCredentials(ctx context.Context, now time.Time, limit int) (out []entitlements.ExpiredCredential, err error) {
	if s.DB == nil || s.Publish == nil {
		return nil, entitlements.ErrCredentialExpiryUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var subscriptions []model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("status = ? AND end_at <= ?", "active", now).
			Order("end_at asc, id asc").Limit(limit).Find(&subscriptions).Error; err != nil {
			return err
		}
		if len(subscriptions) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			ids = append(ids, subscription.ID)
		}
		if err := tx.Model(&model.Subscription{}).
			Where("id IN ? AND status = ? AND end_at <= ?", ids, "active", now).
			Updates(map[string]any{"status": "expired", "updated_at": now}).Error; err != nil {
			return err
		}
		var credentials []model.ProtocolCredential
		if err := tx.Where("status IN ? AND subscription_id IN ?", []string{"active", "prepared"}, ids).Find(&credentials).Error; err != nil {
			return err
		}
		if len(credentials) == 0 {
			return nil
		}
		seen := map[uint]struct{}{}
		credentialIDs := make([]uint, 0, len(credentials))
		out = make([]entitlements.ExpiredCredential, 0, len(credentials))
		for _, credential := range credentials {
			credentialIDs = append(credentialIDs, credential.ID)
			out = append(out, entitlements.ExpiredCredential{
				ID: credential.ID, SubscriptionID: credential.SubscriptionID, NodeID: credential.NodeID,
				ProtocolEndpointID: credential.ProtocolEndpointID,
			})
			if _, ok := seen[credential.NodeID]; ok {
				continue
			}
			seen[credential.NodeID] = struct{}{}
			if err := s.Publish(tx, credential.NodeID, credential.ProtocolEndpointID, 0); err != nil {
				return err
			}
		}
		return tx.Model(&model.ProtocolCredential{}).Where("id IN ?", credentialIDs).
			Updates(map[string]any{"status": "expired", "updated_at": now}).Error
	})
	return
}

func (s CredentialExpiry) HasDueCredentials(ctx context.Context, now time.Time) (bool, error) {
	if s.DB == nil {
		return false, entitlements.ErrCredentialExpiryUnavailable
	}
	var id uint
	err := s.DB.WithContext(ctx).Model(&model.Subscription{}).
		Select("id").Where("status = ? AND end_at <= ?", "active", now).Limit(1).Scan(&id).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return id != 0, nil
}
