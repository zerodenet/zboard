package commercestore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"strings"
)

type OrderQueries struct{ DB *gorm.DB }

func orderReader(tx *gorm.DB, actor uint, admin bool) error {
	if actor == 0 {
		return commerce.ErrOrderPermission
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "status", "is_admin").First(&user, actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return commerce.ErrOrderPermission
		}
		return err
	}
	if user.Status != "active" || (admin && !user.IsAdmin) {
		return commerce.ErrOrderPermission
	}
	return nil
}
func (s OrderQueries) List(ctx context.Context, actor uint, admin bool, q commerce.OrderQuery) (commerce.OrderPage, error) {
	out := commerce.OrderPage{Items: []commerce.OrderListItem{}, Offset: q.Offset, Limit: q.Limit}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, admin); err != nil {
			return err
		}
		query := tx.Model(&model.Order{})
		if !admin {
			query = query.Where("orders.user_id = ?", actor)
		} else if q.UserID != 0 {
			query = query.Where("orders.user_id = ?", q.UserID)
		}
		if admin && q.Search != "" {
			pattern := "%" + strings.ToLower(q.Search) + "%"
			condition := `LOWER(orders.trade_no) LIKE ? OR LOWER(COALESCE(orders.provider_trade_no, '')) LIKE ? OR LOWER(orders.plan_name) LIKE ? OR LOWER(orders.sku_name) LIKE ? OR LOWER(orders.channel) LIKE ?`
			args := []interface{}{pattern, pattern, pattern, pattern, pattern}
			if id, err := strconv.ParseUint(q.Search, 10, 64); err == nil && id > 0 {
				condition += " OR orders.id = ? OR orders.user_id = ? OR orders.subscription_id = ?"
				args = append(args, id, id, id)
			}
			query = query.Where(condition, args...)
		}
		if admin && q.OrderType != "" {
			query = query.Where("orders.order_type = ?", q.OrderType)
		}
		if q.Status != "" {
			statuses, _ := commerce.OrderStatuses(q.Status, admin)
			query = query.Where("orders.status IN ?", statuses)
		}
		if admin && !q.From.IsZero() {
			query = query.Where("orders.created_at >= ? AND orders.created_at < ?", q.From, q.To)
		}
		if !q.LegacyArray {
			if err := query.Count(&out.Total).Error; err != nil {
				return err
			}
			query = query.Offset(q.Offset).Limit(q.Limit)
		}
		// Select only list fields: callback payloads and failure diagnostics never enter this projection.
		return query.Select("orders.id, orders.user_id, orders.subscription_id, orders.plan_id, orders.plan_sku_id, orders.trade_no, orders.order_type, orders.amount_cents, orders.payable_amount, orders.currency, orders.status, orders.plan_name, orders.sku_name, orders.created_at, orders.updated_at").Order("orders.id desc").Scan(&out.Items).Error
	})
	if err != nil {
		return commerce.OrderPage{}, err
	}
	return out, nil
}
func (s OrderQueries) Detail(ctx context.Context, actor, id uint) (commerce.OrderDetail, error) {
	var out commerce.OrderDetail
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var row model.Order
		if err := tx.Omit("raw_callback", "assignment_fingerprint").First(&row, id).Error; err != nil {
			return resourceError(err)
		}
		out = commerce.DetailOrder(commerce.Order(row))
		return nil
	})
	if err != nil {
		return commerce.OrderDetail{}, err
	}
	return out, nil
}
func (s OrderQueries) Events(ctx context.Context, actor, id uint, offset, limit int) (commerce.PaymentEventPage, error) {
	out := commerce.PaymentEventPage{Items: []commerce.PaymentEventSummary{}, Offset: offset, Limit: limit}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var order model.Order
		if err := tx.Select("id").First(&order, id).Error; err != nil {
			return resourceError(err)
		}
		query := tx.Model(&model.PaymentEvent{}).Where("order_id = ?", id)
		if err := query.Count(&out.Total).Error; err != nil {
			return err
		}
		return query.Select("id, provider, provider_event_id, event_type, amount_minor, signature_valid, processed_at, created_at").Order("id desc").Offset(offset).Limit(limit).Scan(&out.Items).Error
	})
	if err != nil {
		return commerce.PaymentEventPage{}, err
	}
	return out, nil
}
