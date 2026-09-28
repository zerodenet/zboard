package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type OrderAssignment struct{ DB *gorm.DB }

func (s OrderAssignment) Assign(ctx context.Context, actor uint, in commerce.AssignedOrderInput) (commerce.Order, error) {
	request := in.Request
	var order model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Audit foreign keys require actor locks. Acquire actor/buyer in stable ID
		// order before the plan, matching settlement's established lock order.
		ids := []uint{request.UserID}
		if actor != request.UserID {
			if actor < request.UserID {
				ids = []uint{actor, request.UserID}
			} else {
				ids = append(ids, actor)
			}
		}
		var administrator model.User
		for _, id := range ids {
			if id == 0 {
				return commerce.ErrOrderPermission
			}
			strength := "SHARE"
			if id == request.UserID {
				strength = "UPDATE"
			}
			var user model.User
			if err := tx.Clauses(clause.Locking{Strength: strength}).Select("id", "email", "status", "is_admin").First(&user, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if id == actor {
						return commerce.ErrOrderPermission
					}
					return &commerce.ValidationError{Message: "订单分配失败。", Fields: map[string]string{"user_id": "用户不存在。"}}
				}
				return err
			}
			if id == actor {
				if !user.IsAdmin || user.Status != "active" {
					return commerce.ErrOrderPermission
				}
				administrator = user
			}
			if id == request.UserID && user.Status != "active" {
				return &commerce.ValidationError{Message: "订单分配失败。", Fields: map[string]string{"user_id": "请先启用该用户。"}}
			}
		}
		err := tx.Where("trade_no = ?", in.TradeNo).First(&order).Error
		if err == nil {
			if order.AssignmentFingerprint != in.Fingerprint {
				return &commerce.ValidationError{Message: "本次操作标识已用于另一笔分配，请重新打开分配窗口。"}
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		created, err := createOrderInTransaction(tx, request.UserID, commerce.OrderCreateRequest{PlanSKUID: request.PlanSKUID, TargetSubscriptionID: request.TargetSubscriptionID, Channel: "admin_assignment", QuoteFingerprint: request.QuoteFingerprint})
		if err != nil {
			return err
		}
		order = created
		if request.PayableAmount != nil {
			order.PayableAmount = *request.PayableAmount
			order.DiscountAmount = max(order.AmountCents-order.PayableAmount, 0)
			if order.ChangeSnapshot != "" {
				var snapshot changeSnapshot
				if err := json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot); err != nil {
					return err
				}
				// An administrator override may waive more, or use less, of the quoted
				// credit. Only credit actually applied transfers to the new entitlement.
				snapshot.Credit = min(snapshot.Credit, order.DiscountAmount)
				payload, err := json.Marshal(snapshot)
				if err != nil {
					return err
				}
				order.ChangeSnapshot = string(payload)
			}
		}
		order.TradeNo = in.TradeNo
		order.AssignedBy = actor
		order.AssignmentNote = request.Note
		order.AssignmentFingerprint = in.Fingerprint
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &administrator.ID, Actor: administrator.Email, Action: "order.assign", Target: fmt.Sprintf("order:%d", order.ID), Detail: fmt.Sprintf("user:%d price:%d payable:%d currency:%s reason:%s", request.UserID, order.AmountCents, order.PayableAmount, order.Currency, request.Note)}).Error
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(order), nil
}

func (s OrderAssignment) Preview(ctx context.Context, actor, buyer uint, request commerce.OrderCreateRequest) (commerce.OrderPreview, error) {
	var preview commerce.OrderPreview
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var administrator model.User
		if err := tx.Select("id", "is_admin", "status").First(&administrator, actor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerce.ErrOrderPermission
			}
			return err
		}
		if !administrator.IsAdmin || administrator.Status != "active" {
			return commerce.ErrOrderPermission
		}
		var err error
		_, preview, err = prepareOrder(tx, buyer, request, time.Now().UTC())
		return err
	})
	return preview, err
}
