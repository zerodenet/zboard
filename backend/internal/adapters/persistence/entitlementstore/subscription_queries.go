package entitlementstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"strings"
	"time"
)

const subscriptionSummaryColumns = `subscriptions.id, subscriptions.user_id, users.email AS user_email,
			subscriptions.plan_id, plans.name AS plan_name,
			subscriptions.plan_sku_id, plan_skus.name AS sku_name,
			subscriptions.node_group_id, subscriptions.subscription_type,
			subscriptions.start_at, subscriptions.end_at, subscriptions.lifecycle, subscriptions.ends_on_quota_exhaustion, subscriptions.ended_at, subscriptions.end_reason,
			CASE
 WHEN subscriptions.status = 'canceled' THEN 'canceled'
 WHEN subscriptions.ended_at IS NOT NULL THEN 'expired'
				WHEN subscriptions.status = 'active'
					AND subscriptions.end_at <= ?
				THEN 'expired'
				WHEN subscriptions.status = 'expired' AND subscriptions.end_at > ? AND subscriptions.flow_used >= subscriptions.flow_total THEN 'active'
				ELSE subscriptions.status
			END AS status,
			CASE WHEN subscriptions.flow_used >= subscriptions.flow_total THEN 'exhausted' ELSE 'available' END AS quota_status,
			subscriptions.reset_quota_bytes,
			subscriptions.flow_total - subscriptions.cycle_start_used AS flow_total,
			subscriptions.flow_used - subscriptions.cycle_start_used AS flow_used,
			subscriptions.speed_limit_mbps, subscriptions.device_limit,
			subscriptions.family_limit, subscriptions.renewal_price_minor,
			subscriptions.reset_policy, subscriptions.next_reset_at,
			subscriptions.traffic_calc_mode, subscriptions.created_at,
			subscriptions.updated_at`

type SubscriptionQueries struct{ DB *gorm.DB }

func subscriptionReader(tx *gorm.DB, actor uint, admin bool) error {
	var user model.User
	if actor == 0 {
		return entitlements.ErrAccessPermission
	}
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "status", "is_admin").First(&user, actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return entitlements.ErrAccessPermission
		}
		return err
	}
	if user.Status != "active" {
		return entitlements.ErrAccessPermission
	}
	if admin && !user.IsAdmin {
		return entitlements.ErrAdministrativeRead
	}
	return nil
}
func subscriptionJoins(query *gorm.DB) *gorm.DB {
	return query.Joins("LEFT JOIN users ON users.id = subscriptions.user_id").Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id")
}
func (s SubscriptionQueries) Detail(ctx context.Context, actor, id uint) (entitlements.SubscriptionDetail, error) {
	var out entitlements.SubscriptionDetail
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := subscriptionReader(tx, actor, true); err != nil {
			return err
		}
		columns, args := subscriptionSummarySelect(time.Now().UTC())
		result := subscriptionJoins(tx.Table("subscriptions")).Select(columns, args...).Where("subscriptions.id = ?", id).Scan(&out.SubscriptionSummary)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return entitlements.ErrAccessNotFound
		}
		if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", id).Count(&out.TotalCredentialCount).Error; err != nil {
			return err
		}
		out.RenewalUntil = entitlements.RenewalDeadline(entitlements.Subscription{Lifecycle: out.Lifecycle, EndAt: out.EndAt, EndedAt: out.EndedAt}, time.Now().UTC())
		return tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status = ?", id, "active").Count(&out.ActiveCredentialCount).Error
	})
	if err != nil {
		return entitlements.SubscriptionDetail{}, err
	}
	return out, nil
}
func (s SubscriptionQueries) List(ctx context.Context, actor uint, admin bool, q entitlements.SubscriptionQuery) (entitlements.SubscriptionPage, error) {
	out := entitlements.SubscriptionPage{Items: []entitlements.SubscriptionSummary{}, Legacy: []entitlements.Subscription{}, Offset: q.Offset, Limit: q.Limit}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := subscriptionReader(tx, actor, admin); err != nil {
			return err
		}
		now := time.Now().UTC()
		query := tx.Model(&model.Subscription{})
		if admin || !q.LegacyArray {
			query = subscriptionJoins(query)
		}
		owner := q.UserID
		if !admin {
			owner = actor
		}
		if owner != 0 {
			query = query.Where("subscriptions.user_id = ?", owner)
		}
		if q.ID != 0 {
			query = query.Where("subscriptions.id = ?", q.ID)
		}
		query = subscriptionEligibility(query, q.EligibleFor, now)
		if q.Status != "" {
			query = subscriptionStatus(query, q.Status, now)
		}
		if (admin || !q.LegacyArray) && q.Search != "" {
			pattern := "%" + strings.ToLower(q.Search) + "%"
			condition := "LOWER(plans.name) LIKE ? OR LOWER(plan_skus.name) LIKE ?"
			args := []interface{}{pattern, pattern}
			if admin {
				condition += " OR LOWER(users.email) LIKE ?"
				args = append(args, pattern)
			}
			if id, err := strconv.ParseUint(q.Search, 10, 64); err == nil && id > 0 {
				condition += " OR subscriptions.id = ?"
				args = append(args, id)
				if admin {
					condition += " OR subscriptions.user_id = ? OR subscriptions.plan_id = ?"
					args = append(args, id, id)
				}
			}
			query = query.Where(condition, args...)
		}
		if admin {
			if q.Quota == "available" {
				query = query.Where("subscriptions.flow_used < subscriptions.flow_total")
			} else if q.Quota == "exhausted" {
				query = query.Where("subscriptions.flow_used >= subscriptions.flow_total")
			}
			if !q.From.IsZero() {
				query = query.Where("subscriptions.end_at >= ? AND subscriptions.end_at < ?", q.From, q.To)
			}
		}
		if !q.LegacyArray {
			if err := query.Count(&out.Total).Error; err != nil {
				return err
			}
			columns, args := subscriptionSummarySelect(now)
			if err := query.Select(columns, args...).Order("subscriptions.id desc").Offset(q.Offset).Limit(q.Limit).Scan(&out.Items).Error; err != nil {
				return err
			}
			for index := range out.Items {
				item := &out.Items[index]
				item.RenewalUntil = entitlements.RenewalDeadline(entitlements.Subscription{Lifecycle: item.Lifecycle, EndAt: item.EndAt, EndedAt: item.EndedAt}, now)
			}
			return nil
		}
		var rows []model.Subscription
		if err := query.Select("subscriptions.*").Order("subscriptions.id desc").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			item := entitlements.Subscription(row)
			item.Status = entitlements.EffectiveStatus(item, now)
			item.FlowTotal, item.FlowUsed = entitlements.CycleQuota(item)
			out.Legacy = append(out.Legacy, item)
		}
		return nil
	})
	if err != nil {
		return entitlements.SubscriptionPage{}, err
	}
	return out, nil
}
func subscriptionStatus(query *gorm.DB, status string, now time.Time) *gorm.DB {
	switch status {
	case "active":
		return query.Where("subscriptions.ended_at IS NULL AND (subscriptions.status = ? OR (subscriptions.status = ? AND subscriptions.flow_used >= subscriptions.flow_total)) AND subscriptions.end_at > ?", "active", "expired", now)
	case "expired":
		return query.Where("(subscriptions.ended_at IS NOT NULL OR (subscriptions.status IN ? AND subscriptions.end_at <= ?) OR (subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total))", []string{"active", "expired"}, now, "expired", now)
	default:
		return query.Where("subscriptions.status = ?", status)
	}
}
func subscriptionEligibility(query *gorm.DB, purpose string, now time.Time) *gorm.DB {
	if purpose == "manage" {
		var predicates []string
		var args []any
		for _, operation := range []string{"renew", "change", "addon", "reset"} {
			predicate, values := subscriptionOperationPredicate(operation, now)
			predicates = append(predicates, predicate)
			args = append(args, values...)
		}
		return query.Where("("+strings.Join(predicates, " OR ")+")", args...)
	}
	if predicate, args := subscriptionOperationPredicate(purpose, now); predicate != "" {
		return query.Where(predicate, args...)
	}
	return query
}
