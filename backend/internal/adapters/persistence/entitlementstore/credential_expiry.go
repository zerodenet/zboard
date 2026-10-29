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

// Each subscription retains one complete financial transaction. A single
// oversized history may be deferred after rollback without rolling back another
// subscription's independently valid closure.
func (s CredentialExpiry) ExpireDueCredentials(ctx context.Context, now time.Time, limit int) ([]entitlements.ExpiredCredential, error) {
	if ctx == nil || s.DB == nil || s.Publish == nil || s.BeforeRetire == nil || s.PrepareScope == nil || now.IsZero() {
		return nil, entitlements.ErrCredentialExpiryUnavailable
	}
	if limit < 1 || limit > 200 {
		return nil, errors.New("credential expiry batch must be between1 and200")
	}
	grace, err := RecoveryGracePeriod(s.DB.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	var candidates []model.Subscription
	// Both supported databases order NULL before non-NULL in ascending order.
	// Never-attempted subscriptions therefore advance past even already-due retry
	// rows; a persistent retry time also survives worker restarts.
	if err := subscriptionLifecycleDue(s.DB.WithContext(ctx), now, grace).
		Select("id", "user_id", "entitlement_epoch", "start_at", "end_at", "ended_at").
		Order("retirement_retry_at ASC, id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	var out []entitlements.ExpiredCredential
	var deferred []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return out, errors.Join(append(deferred, err)...)
		}
		scope, err := s.PrepareScope(ctx, []model.Subscription{candidate})
		var committed []entitlements.ExpiredCredential
		if err == nil {
			if scope.Lock == nil || scope.Validate == nil {
				return out, errors.Join(append(deferred, entitlements.ErrCredentialExpiryUnavailable)...)
			}
			err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				// Preserve the existing order -> owner -> subscription lock order and
				// establish MySQL's consistent-read snapshot only after the owner lock.
				if err := scope.Lock(tx); err != nil {
					return err
				}
				grace, err := RecoveryGracePeriod(tx)
				if err != nil {
					return err
				}
				var subs []model.Subscription
				if err := subscriptionLifecycleDue(tx.Clauses(clause.Locking{Strength: "UPDATE"}), now, grace).
					Where("id = ?", candidate.ID).Limit(1).Find(&subs).Error; err != nil {
					return err
				}
				if len(subs) == 0 {
					return nil
				}
				sub := subs[0]
				if candidate.UserID != sub.UserID {
					return fmt.Errorf("expiry subscription owner changed before lock; retry")
				}
				if err := scope.Validate(tx, subs); err != nil {
					return err
				}
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
					committed = append(committed, entitlements.ExpiredCredential{ID: credential.ID, SubscriptionID: sub.ID, NodeID: credential.NodeID, ProtocolEndpointID: credential.ProtocolEndpointID})
					if !seen[credential.NodeID] {
						if err := s.Publish(tx, credential.NodeID, credential.ProtocolEndpointID, 0); err != nil {
							return err
						}
						seen[credential.NodeID] = true
					}
				}
				if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", sub.ID, []string{"active", "prepared"}).
					Updates(map[string]any{"status": "expired", "updated_at": now}).Error; err != nil {
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
				return tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).UpdateColumn("retirement_retry_at", nil).Error
			})
		}
		if err == nil {
			out = append(out, committed...)
			continue
		}
		reason := entitlements.CredentialExpiryBudgetReason(err)
		if reason == "" {
			return out, errors.Join(append(deferred, err)...)
		}
		// The entire failed financial transaction has already rolled back. Only its
		// operational retry schedule/audit can now be committed separately.
		if holdErr := s.deferRetirement(ctx, candidate, now, reason); holdErr != nil {
			return out, errors.Join(append(deferred, err, holdErr)...)
		}
		deferred = append(deferred, fmt.Errorf("subscription %d: %w", candidate.ID, err))
	}
	return out, errors.Join(deferred...)
}

func (s CredentialExpiry) HasDueCredentials(ctx context.Context, now time.Time) (bool, error) {
	if s.DB == nil {
		return false, entitlements.ErrCredentialExpiryUnavailable
	}
	var id uint
	if err := dueAccessCredentials(s.DB.WithContext(ctx), now).Select("protocol_credentials.id").Limit(1).Scan(&id).Error; err != nil {
		return false, err
	}
	if id != 0 {
		return true, nil
	}
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
