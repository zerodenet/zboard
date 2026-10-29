package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExpireDueAccessCredentials commits only local credential expiry and durable
// publication intent. Expensive financial closure remains in ExpireDueCredentials
// with its original order -> user -> subscription lock/transaction boundary.
// A committed credential leaves this query, so even a restarted scan advances
// past old subscriptions whose financial history exceeds a separate budget.
func (s CredentialExpiry) ExpireDueAccessCredentials(ctx context.Context, now time.Time, limit int) (int, error) {
	if ctx == nil || s.DB == nil || s.Publish == nil || now.IsZero() {
		return 0, entitlements.ErrCredentialExpiryUnavailable
	}
	if limit < 1 || limit > 200 {
		return 0, errors.New("credential access expiry batch must be between1 and200")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	type candidate struct{ ID, SubscriptionID, UserID uint }
	var candidates []candidate
	if err := dueAccessCredentials(s.DB.WithContext(ctx), now).
		Select("protocol_credentials.id,protocol_credentials.subscription_id,subscriptions.user_id").
		Order("protocol_credentials.id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}
	groups := map[uint][]uint{}
	owners := map[uint]uint{}
	for _, c := range candidates {
		if c.ID == 0 || c.SubscriptionID == 0 || c.UserID == 0 {
			return 0, entitlements.ErrCredentialExpiryUnavailable
		}
		groups[c.SubscriptionID] = append(groups[c.SubscriptionID], c.ID)
		owners[c.SubscriptionID] = c.UserID
	}
	ids := make([]uint, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	changed := 0
	for _, id := range ids {
		n := 0
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// No financial order is acquired after this user lock. Current reads
			// wait for any renewal/grant and then recheck its actual new deadline.
			var user model.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, owners[id]).Error; err != nil {
				return err
			}
			var sub model.Subscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; err != nil {
				return err
			}
			if sub.UserID != owners[id] {
				return fmt.Errorf("access expiry subscription owner changed before lock; retry")
			}
			if (sub.Status != "active" && sub.Status != "expired") || sub.EndAt.After(now) {
				return nil
			}
			query := tx.Model(&model.ProtocolCredential{}).Where("id IN ? AND subscription_id=? AND status IN ?", groups[id], sub.ID, []string{"active", "prepared"}).Order("id ASC").Limit(limit)
			var err error
			n, err = persistCredentialRevocationWithPublisher(tx, query, "expired", now, s.Publish)
			if err != nil || n == 0 {
				return err
			}
			return tx.Create(&model.AuditLog{UserID: &sub.UserID, Actor: "system", Action: "subscription.access_expire_intent", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("credentials=%d; observed_at=%s; local credential expiry and durable publication intent committed together; remote application/connection confirmation remains pending", n, now.UTC().Format(time.RFC3339Nano))}).Error
		})
		if err != nil {
			return changed, err
		}
		changed += n
	}
	return changed, nil
}

// Access revocation deliberately ignores financial retry scheduling.
func dueAccessCredentials(query *gorm.DB, now time.Time) *gorm.DB {
	return query.Table("protocol_credentials").
		Joins("JOIN subscriptions ON subscriptions.id=protocol_credentials.subscription_id").
		Where("protocol_credentials.status IN ? AND subscriptions.status IN ?", []string{"active", "prepared"}, []string{"active", "expired"}).
		Where("subscriptions.end_at<=?", now)
}
