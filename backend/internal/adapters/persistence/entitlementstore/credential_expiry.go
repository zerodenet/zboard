package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CredentialExpiryScope struct {
	Lock     func(*gorm.DB) error
	Validate func(*gorm.DB, []model.Subscription) error
}

type CredentialExpiry struct {
	DB           *gorm.DB
	Cipher       entitlements.AccessCipher
	Publish      func(*gorm.DB, uint, uint, uint) error
	BeforeRetire func(*gorm.DB, model.Subscription, time.Time) error
	PrepareScope func(context.Context, []model.Subscription) (CredentialExpiryScope, error)
}

func (s CredentialExpiry) ExpireDueCredentials(ctx context.Context, now time.Time, limit int) (out []entitlements.ExpiredCredential, err error) {
	if s.DB == nil || s.Publish == nil || s.BeforeRetire == nil || s.PrepareScope == nil {
		return nil, entitlements.ErrCredentialExpiryUnavailable
	}
	grace, err := RecoveryGracePeriod(s.DB.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	var candidates []model.Subscription
	if err := subscriptionLifecycleDue(s.DB.WithContext(ctx), now, grace).Select("id", "user_id").Order("id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	var scope CredentialExpiryScope
	for {
		scope, err = s.PrepareScope(ctx, candidates)
		if errors.Is(err, entitlements.ErrCredentialExpiryScopeBudget) && len(candidates) > 1 {
			candidates = candidates[:(len(candidates)+1)/2]
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	if scope.Lock == nil || scope.Validate == nil {
		return nil, entitlements.ErrCredentialExpiryUnavailable
	}
	ids := make([]uint, 0, len(candidates))
	owners := map[uint]uint{}
	for _, sub := range candidates {
		ids = append(ids, sub.ID)
		owners[sub.ID] = sub.UserID
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := scope.Lock(tx); err != nil {
			return err
		}
		grace, err := RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		var subscriptions []model.Subscription
		if err := subscriptionLifecycleDue(tx.Clauses(clause.Locking{Strength: "UPDATE"}), now, grace).Where("id IN ?", ids).Order("id asc").Limit(limit).Find(&subscriptions).Error; err != nil {
			return err
		}
		for _, sub := range subscriptions {
			if owners[sub.ID] != sub.UserID {
				return fmt.Errorf("expiry subscription owner changed before lock; retry")
			}
		}
		if err := scope.Validate(tx, subscriptions); err != nil {
			return err
		}
		for _, sub := range subscriptions {
			if err := endSubscription(tx, &sub, now); err != nil {
				return err
			}
			deadline := entitlements.RenewalDeadline(entitlements.Subscription(sub), now, grace)
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
				if err := s.BeforeRetire(tx, sub, now); err != nil {
					return err
				}
				if err := retireSubscription(tx, sub, s.Cipher, now); err != nil {
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
	grace, err := RecoveryGracePeriod(s.DB.WithContext(ctx))
	if err != nil {
		return false, err
	}
	err = subscriptionLifecycleDue(query, now, grace).Scan(&id).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return id != 0, nil
}
