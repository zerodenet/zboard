package commercestore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// createWithoutRedemptionConfirmation retains the original authorization,
// subscription scope and order-creation transaction. Confirmation runs only
// after that canonical order+intent transaction has successfully committed.
func (s OrderCreation) Create(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.Order, error) {
	out, err := s.createWithoutRedemptionConfirmation(ctx, buyer, request)
	if err != nil {
		return out, err
	}
	required, _, err := OrderDiscountRedemptionRequirement(model.Order(out))
	if err != nil {
		return commerce.Order{}, err
	}
	if !required || out.Status != "pending" {
		return out, nil
	}
	provider, ok := s.Discounts.(commerce.DiscountRedemptionProvider)
	if ok {
		// Persisted pending order remains retrievable/cancelable if the network fails.
		// A worker reconciles this same key; no automatic higher-price replacement.
		_ = (DiscountRedemptionJobs{DB: s.DB, Provider: provider, Now: s.Now}).Step(ctx, out.ID)
	}
	if err := RequireOrderDiscountReservation(s.DB.WithContext(ctx), model.Order(out), commerceNow(s.Now)); err != nil {
		out.FailureReason = "优惠预留尚未确认，订单金额保持不变且不能付款；可稍后重试或取消订单。"
	}
	return out, nil
}

func discountReservationValidation(err error) error {
	return errors.Join(err, &commerce.ValidationError{Message: "优惠预留尚未确认或已撤销，未扣款；请刷新原订单，稍后重试或取消，不会恢复原价。", Fields: map[string]string{"discount_reservation": "unconfirmed"}})
}

func CommitOrderDiscountRedemptionIntent(tx *gorm.DB, order model.Order, now time.Time) error {
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	return CommitDiscountRedemptionIntent(tx, order.ID, now)
}

func ReleaseClosingOrderDiscountIntent(tx *gorm.DB, order model.Order, now time.Time) error {
	if order.Status == "paid" || order.Status == "refunded" {
		return nil
	}
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	// All callers of releaseOrderFunding atomically close/replace the order in
	// their enclosing transaction. This shadow status expresses that same intent
	// before the caller writes its terminal order status later in the transaction.
	order.Status = "canceled"
	return ReleaseDiscountRedemptionIntent(tx, order, now)
}

// Reuse the existing 30 minute unpaid funding lifetime. A configured funding
// reservation retains its own later deadline. The provider itself never expires
// its hold; core first closes payability and durably requests Release.
func (s DiscountRedemptionJobs) ExpireUnpaid(ctx context.Context, limit int) (int, error) {
	now := commerceNow(s.Now)
	var ids []uint
	if err := s.DB.WithContext(ctx).Table("order_discount_redemptions AS redemption").Joins("LEFT JOIN order_fundings AS funding ON funding.order_id = redemption.order_id").Where("redemption.desired = ? AND redemption.created_at <= ? AND redemption.next_attempt_at <= ? AND (funding.order_id IS NULL OR funding.state <> ? OR funding.expires_at <= ?)", "reserve", now.Add(-orderFundingLifetime), now, "reserved", now).Order("redemption.created_at, redemption.order_id").Limit(limit).Pluck("redemption.order_id", &ids).Error; err != nil {
		return 0, err
	}
	expired := 0
	var failures error
	for _, id := range ids {
		changed := false
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var order model.Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, id).Error; err != nil {
				return err
			}
			if order.Status != "pending" && order.Status != "failed" {
				return nil
			}
			var row model.OrderDiscountRedemption
			if err := loadDiscountRedemption(tx, id, true, &row); err != nil {
				return err
			}
			if row.Desired != "reserve" || row.CreatedAt.After(now.Add(-orderFundingLifetime)) {
				return nil
			}
			funding, exists, err := findOrderFunding(tx, order)
			if err != nil {
				return err
			}
			if exists && funding.State == "reserved" && funding.ExpiresAt.After(now) {
				return nil
			}
			if err := releaseOrderFunding(tx, order, "expired", now); err != nil {
				return err
			}
			if err := tx.Model(&order).Updates(map[string]any{"status": "canceled", "canceled_at": now, "failure_reason": "未付款优惠订单已失效，未扣款；优惠预留将在后台可靠释放。", "updated_at": now}).Error; err != nil {
				return err
			}
			changed = true
			return tx.Create(&model.AuditLog{Actor: "system", Action: "commerce.discount.expire", Target: "order:" + strconv.FormatUint(uint64(order.ID), 10), Detail: "unpaid exclusive discount hold closed and release intent persisted"}).Error
		})
		if err != nil {
			failures = errors.Join(failures, err)
			// Isolate one damaged/orphaned order, retain its immutable evidence, and
			// let independent commit/release jobs continue. No deletion or repair by guess.
			retry := s.DB.WithContext(ctx).Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", id).Updates(map[string]any{"next_attempt_at": now.Add(time.Minute), "last_error": "expiry_reconciliation_required", "updated_at": now})
			failures = errors.Join(failures, retry.Error)
			continue
		}
		if changed {
			expired++
		}
	}
	return expired, failures
}

func orderDiscountReservationView(tx *gorm.DB, order model.Order, now time.Time, cashMinor int64) (*commerce.DiscountReservationView, error) {
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return nil, err
	}
	if !required {
		return nil, nil
	}
	view := &commerce.DiscountReservationView{Required: true, CanCancel: order.Status == "pending", State: "pending", Message: "优惠预留尚未确认，不能付款；订单金额保持不变，可稍后核对或取消。"}
	// Reuse the already validated funding split. This point lookup neither
	// creates a collection attempt nor reads provider credentials/large bodies.
	if cashMinor > 0 {
		var attempt struct{ OrderID uint }
		found := tx.Model(&model.PaymentAttempt{}).Select("order_id").Where("order_id = ? AND currency = ? AND amount_minor = ?", order.ID, order.Currency, cashMinor).Limit(1).Scan(&attempt)
		if found.Error != nil {
			return nil, found.Error
		}
		view.CanReconcile = found.RowsAffected == 1
	}
	var row model.OrderDiscountRedemption
	if err := loadDiscountRedemption(tx, order.ID, false, &row); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return view, nil
		}
		return nil, err
	}
	if err := validateDiscountReservationOrder(row, order); err != nil {
		return nil, err
	}
	switch {
	case row.Token == "" && row.LastError == "provider_redemption_unsupported":
		view.State = "unavailable"
		view.Message = "优惠提供方不支持可靠的排他预留，不能付款；请取消本单后重新确认报价，不会恢复原价。"
	case row.Desired == "reserve" && row.Observed == "reserved" && row.Token != "":
		view.State = "confirmed"
		view.CanPay = discountReservationCanPay(row, order)
		window, err := discountReservationWindowOpen(tx, row, order, now)
		if err != nil {
			return nil, err
		}
		view.CanPay = view.CanPay && window
		if !window {
			view.State = "unavailable"
			view.Message = "本次优惠付款窗口已结束，不能付款；请取消并重新确认报价，后台将可靠释放预留。"
			return view, nil
		}
		view.Message = "优惠已确认保留，付款按订单已确认金额执行。"
	case row.Desired == "commit":
		view.State = "settled"
		view.Message = "订单优惠已使用，后续确认在后台恢复；不会重复扣款。"
	case row.Desired == "release":
		view.State = "closed"
		view.Message = "本次优惠订单已关闭，不再收款；已核验的迟到付款会单独入账核对。"
	case row.Observed == "rejected" || row.Observed == "released" || row.Observed == "conflict":
		view.State = "unavailable"
		view.Message = "本次优惠无法确认，未扣款且不会按原价付款；请取消后重新确认报价。"
	}
	return view, nil
}

// Verified cash must remain a reliable financial fact even when reservation
// authority is damaged. Preserve this existing controlled review disposition;
// do not guess a hold identity, release another order's stock or discard cash.
func preserveReceiptWithDamagedDiscountAuthority(tx *gorm.DB, order model.Order, receipt *model.PaymentReceipt) (bool, error) {
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err == nil && !required {
		return false, nil
	}
	if err == nil {
		var row model.OrderDiscountRedemption
		err = loadDiscountRedemption(tx, order.ID, true, &row)
		if err == nil {
			err = validateDiscountReservationOrder(row, order)
		}
	}
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, commerce.ErrDiscountRedemptionConflict) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	receipt.Disposition = "review_required"
	return true, tx.Model(receipt).Update("disposition", receipt.Disposition).Error
}
