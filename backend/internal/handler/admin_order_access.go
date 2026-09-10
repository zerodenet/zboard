package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Read only the credential for the subscription actually fulfilled by this order.
// Missing scoped credentials use the same core provisioner as the account page.
// No account-wide token, rotation or reactivation is permitted here.
func (h *handlers) AdminOrderSubscriptionAccessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/subscription-access") {
		BadRequest(w, "invalid order access path")
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/subscription-access"), "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var view subscriptionAccessView
	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&order, id).Error; err != nil {
			return err
		}
		view.SubscriptionID = order.SubscriptionID
		view.Notice = "订单尚未完成收款和履约，暂不能获取订阅地址。"
		if order.Status != orderStatusPaid || order.FulfilledAt == nil || order.SubscriptionID == 0 {
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
		if user.Status != userStatusActive || !subscriptionAccessAvailable(subscription, time.Now().UTC()) {
			return nil
		}
		access, raw, err := h.createMissingSubscriptionAccessTokenIn(tx, subscription)
		view.Notice = "订阅地址已撤销或不可读取，请由客户在个人中心管理。"
		if err != nil {
			return err
		}
		if access.RevokedAt != nil || access.TokenCiphertext == "" {
			return nil
		}
		if raw == "" {
			raw, err = h.readableSubscriptionToken(&access)
			if err != nil {
				return err
			}
		}
		if err := createAuditLog(tx, claims, "order.subscription_access", fmt.Sprintf("order:%d", id), fmt.Sprintf("subscription:%d", subscription.ID)); err != nil {
			return err
		}
		view = h.accessView(access, raw, true)
		view.Token = "" // Only the scoped URL is needed for delivery.
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, view)
}
