package commercestore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type OrderAccess struct {
	DB     *gorm.DB
	Cipher entitlements.AccessCipher
}

func (s OrderAccess) Read(ctx context.Context, actor, id uint) (entitlements.AccessView, error) {
	var view entitlements.AccessView
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&order, id).Error; err != nil {
			return resourceError(err)
		}
		administrator, err := lockSettlementUsers(tx, order.UserID, actor)
		if err != nil {
			return err
		}
		view.SubscriptionID = order.SubscriptionID
		view.Notice = "订单尚未完成收款和履约，暂不能获取订阅地址。"
		if order.Status != "paid" || order.FulfilledAt == nil || order.SubscriptionID == 0 {
			return nil
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&user, order.UserID).Error; err != nil {
			return err
		}
		var subscription model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", order.SubscriptionID, order.UserID).First(&subscription).Error; err != nil {
			return err
		}
		view.Notice = "客户或订阅已停用、到期或流量耗尽，暂不能获取订阅地址。"
		if user.Status != "active" || !entitlements.AccessAvailable(entitlements.Subscription(subscription), time.Now().UTC()) {
			return nil
		}
		access, raw, err := entitlementstore.EnsureAccessToken(tx, subscription, s.Cipher)
		view.Notice = "订阅地址已撤销或不可读取，请由客户在个人中心管理。"
		if err != nil {
			return err
		}
		if access.RevokedAt != nil || access.TokenCiphertext == "" {
			return nil
		}
		if raw == "" {
			raw, err = entitlements.RecoverAccessToken(s.Cipher, access.TokenCiphertext, access.TokenHash)
			if err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &administrator.ID, Actor: administrator.Email, Action: "order.subscription_access", Target: fmt.Sprintf("order:%d", id), Detail: fmt.Sprintf("subscription:%d", subscription.ID)}).Error; err != nil {
			return err
		}
		view = entitlementstore.AccessView(access, raw, true)
		return nil
	})
	if err != nil {
		return entitlements.AccessView{}, resourceError(err)
	}
	return view, nil
}
