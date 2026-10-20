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
	sub, _, err := FulfillWithPrevious(tx, order, policy, issuer, now)
	return sub, err
}

// FulfillWithPrevious returns the exact business state after due resets and
// before this grant. Commerce uses it to record actual paid term/value evidence
// in the same transaction, without reconstructing old state from today's plan.
func FulfillWithPrevious(tx *gorm.DB, order entitlements.GrantRequest, policy entitlements.GrantPolicy, issuer entitlements.CredentialIssuer, now time.Time) (model.Subscription, *model.Subscription, error) {
	if order.OrderType != "new" && order.TargetSubscriptionID == nil {
		return model.Subscription{}, nil, entitlements.ErrGrantTargetRequired
	}
	if order.OrderType == "renewal" && policy.RecoveryGracePeriod == nil {
		grace, err := RecoveryGracePeriod(tx)
		if err != nil {
			return model.Subscription{}, nil, err
		}
		policy.RecoveryGracePeriod = &grace
	}
	if err := ExpireInTransaction(tx, order.UserID, now); err != nil {
		return model.Subscription{}, nil, err
	}

	var sub model.Subscription
	var previous *model.Subscription
	var err error
	if order.OrderType == "new" && order.TargetSubscriptionID == nil {
		// A purchase grants its own subscription; only renewal/legacy orders
		// may resolve an existing same-SKU subscription implicitly.
		err = gorm.ErrRecordNotFound
	} else if order.TargetSubscriptionID != nil {
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Subscription{}, nil, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if order.TargetSubscriptionID != nil {
			return model.Subscription{}, nil, errors.New("target subscription is unavailable")
		}
		if err := CheckCapacity(tx, order.PlanID, policy.MaxActiveSubscriptions, now); err != nil {
			return model.Subscription{}, nil, err
		}
		grant, err := entitlements.NewGrant(order, policy, now)
		if err != nil {
			return model.Subscription{}, nil, err
		}
		sub = model.Subscription(grant.Subscription)
		if err := tx.Create(&sub).Error; err != nil {
			return model.Subscription{}, nil, err
		}
		if _, err := EnsureCredentials(tx, sub, issuer); err != nil {
			return model.Subscription{}, nil, err
		}
		if err := RecordQuotaEvent(tx, sub, "purchase", order.TrafficBytes, 0, sub.FlowTotal, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
			return model.Subscription{}, nil, err
		}
		return sub, previous, nil
	}

	previousGroupID := sub.NodeGroupID
	// Recovery must close the previous period even when the expiry worker has
	// not run yet. Preserve its historical orders before restoring this instance.
	recovering := order.OrderType == "renewal" && (sub.EndedAt != nil || !sub.EndAt.After(now))
	if recovering {
		if err := endSubscription(tx, &sub, now); err != nil {
			return model.Subscription{}, nil, err
		}
	}
	if !recovering {
		if _, err := ApplyDueTrafficReset(tx, &sub, now); err != nil {
			return model.Subscription{}, nil, err
		}
	}
	if order.ReplaceTerm && sub.PlanID != order.PlanID {
		if err := CheckCapacity(tx, order.PlanID, policy.MaxActiveSubscriptions, now); err != nil {
			return model.Subscription{}, nil, err
		}
	}
	prior := sub
	previous = &prior
	grant, err := entitlements.ApplyGrant(entitlements.Subscription(sub), order, policy, now)
	if err != nil {
		return model.Subscription{}, nil, err
	}
	sub = model.Subscription(grant.Subscription)
	if recovering || order.ReplaceTerm {
		if err := closeEntitlement(tx, prior, order.ID, issuer, now); err != nil {
			return model.Subscription{}, nil, err
		}
		sub.EntitlementEpoch = prior.EntitlementEpoch + 1
	}

	if err := tx.Save(&sub).Error; err != nil {
		return model.Subscription{}, nil, err
	}
	if sub.NodeGroupID != previousGroupID {
		if err := RevokeOutsideGroup(tx, sub, now); err != nil {
			return model.Subscription{}, nil, err
		}
	}
	if _, err := EnsureCredentials(tx, sub, issuer); err != nil {
		return model.Subscription{}, nil, err
	}
	eventType := order.OrderType
	if eventType == "upgrade" {
		// Replacement is already represented by ResetQuotaBytes. A late reset
		// must not replay this delta as an additional purchase of quota.
		eventType = "plan_change"
	}
	if err := RecordQuotaEvent(tx, sub, eventType, grant.QuotaDelta, grant.BalanceBefore, grant.BalanceAfter, "order", strconv.FormatUint(uint64(order.ID), 10)); err != nil {
		return model.Subscription{}, nil, err
	}
	return sub, previous, nil
}
