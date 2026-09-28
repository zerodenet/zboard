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
		if err := subscriptionLifecycleDue(tx.Clauses(clause.Locking{Strength: "UPDATE"}), now).Order("id asc").Limit(limit).Find(&subscriptions).Error; err != nil {
			return err
		}
		for _, sub := range subscriptions {
			if err := endSubscription(tx, &sub, now); err != nil {
				return err
			}
			deadline := entitlements.RenewalDeadline(entitlements.Subscription(sub), now)
			cleanup := deadline == nil || !deadline.After(now)
			var credentials []model.ProtocolCredential
			query := tx.Where("subscription_id = ?", sub.ID)
			if !cleanup {
				query = query.Where("status IN ?", []string{"active", "prepared"})
			}
			if err := query.Find(&credentials).Error; err != nil {
				return err
			}
			seen := map[uint]bool{}
			for _, credential := range credentials {
				out = append(out, entitlements.ExpiredCredential{ID: credential.ID, SubscriptionID: sub.ID, NodeID: credential.NodeID, ProtocolEndpointID: credential.ProtocolEndpointID})
				if !seen[credential.NodeID] {
					if err := s.Publish(tx, credential.NodeID, credential.ProtocolEndpointID, 0); err != nil {
						return err
					}
					seen[credential.NodeID] = true
				}
			}
			if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", sub.ID, []string{"active", "prepared"}).Updates(map[string]any{"status": "expired", "updated_at": now}).Error; err != nil {
				return err
			}
			if cleanup {
				if err := retireSubscription(tx, sub, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return
}

func (s CredentialExpiry) HasDueCredentials(ctx context.Context, now time.Time) (bool, error) {
	if s.DB == nil {
		return false, entitlements.ErrCredentialExpiryUnavailable
	}
	var id uint
	query := s.DB.WithContext(ctx).Model(&model.Subscription{}).Select("id").Limit(1)
	err := subscriptionLifecycleDue(query, now).Scan(&id).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return id != 0, nil
}
