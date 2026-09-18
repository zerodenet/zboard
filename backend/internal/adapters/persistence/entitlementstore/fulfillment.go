package entitlementstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

func Fulfill(tx *gorm.DB, order entitlements.GrantRequest, policy entitlements.GrantPolicy, issuer entitlements.CredentialIssuer, now time.Time) (model.Subscription, error) {
	if err := ExpireInTransaction(tx, order.UserID, now); err != nil {
		return model.Subscription{}, err
	}

	var sub model.Subscription
	var err error
	if order.OrderType == "new" && order.TargetSubscriptionID == nil {
		// A purchase grants its own subscription; only renewal/legacy orders
		// may resolve an existing same-SKU subscription implicitly.
		err = gorm.ErrRecordNotFound
	} else if order.TargetSubscriptionID != nil {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error
	} else {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND plan_sku_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", order.UserID, order.PlanSKUID, "active", now).
			Order("end_at desc").First(&sub).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Subscription{}, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if order.TargetSubscriptionID != nil {
			return model.Subscription{}, errors.New("target subscription is unavailable")
		}
		if err := CheckCapacity(tx, order.PlanID, policy.MaxActiveSubscriptions, now); err != nil {
			return model.Subscription{}, err
		}
		grant, err := entitlements.NewGrant(order, policy, now)
		if err != nil {
			return model.Subscription{}, err
		}
		sub = model.Subscription(grant.Subscription)
		if err := tx.Create(&sub).Error; err != nil {
			return model.Subscription{}, err
		}
		if _, err := EnsureCredentials(tx, sub, issuer); err != nil {
			return model.Subscription{}, err
		}
		if err := RecordQuotaEvent(tx, sub, "purchase", order.TrafficBytes, 0, sub.FlowTotal, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
			return model.Subscription{}, err
		}
		return sub, nil
	}

	previousGroupID := sub.NodeGroupID
	grant, err := entitlements.ApplyGrant(entitlements.Subscription(sub), order, policy, now)
	if err != nil {
		return model.Subscription{}, err
	}
	sub = model.Subscription(grant.Subscription)

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, err
	}
	if sub.NodeGroupID != previousGroupID {
		if err := RevokeOutsideGroup(tx, sub, now); err != nil {
			return model.Subscription{}, err
		}
	}
	if _, err := EnsureCredentials(tx, sub, issuer); err != nil {
		return model.Subscription{}, err
	}
	if err := RecordQuotaEvent(tx, sub, order.OrderType, grant.QuotaDelta, grant.BalanceBefore, grant.BalanceAfter, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
		return model.Subscription{}, err
	}
	return sub, nil
}
