package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// adminOrderListItem is the bounded order projection used by both account and
// administration tables. Payment callback bodies and failure diagnostics are
// detail-only and must never be returned by a list endpoint.
type adminOrderListItem struct {
	ID             uint      `json:"id"`
	UserID         uint      `json:"user_id"`
	SubscriptionID uint      `json:"subscription_id"`
	PlanID         uint      `json:"plan_id"`
	PlanSKUID      uint      `json:"plan_sku_id"`
	TradeNo        string    `json:"trade_no"`
	OrderType      string    `json:"order_type"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	PlanName       string    `json:"plan_name"`
	SKUName        string    `json:"sku_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type adminOrderDetail struct {
	adminOrderListItem
	TargetSubscriptionID *uint      `json:"target_subscription_id"`
	PayableAmount        int64      `json:"payable_amount"`
	PaidAmount           int64      `json:"paid_amount"`
	RefundAmount         int64      `json:"refund_amount"`
	DiscountAmount       int64      `json:"discount_amount"`
	Channel              string     `json:"channel"`
	ProviderTradeNo      *string    `json:"provider_trade_no"`
	BillingUnit          string     `json:"billing_unit"`
	BillingValue         int        `json:"billing_value"`
	RenewalEffect        string     `json:"renewal_effect"`
	TrafficBytes         int64      `json:"traffic_bytes"`
	DeviceLimit          int        `json:"device_limit"`
	SpeedLimitMbps       int        `json:"speed_limit_mbps"`
	PaidAt               *time.Time `json:"paid_at"`
	CanceledAt           *time.Time `json:"canceled_at"`
	FulfilledAt          *time.Time `json:"fulfilled_at"`
	RefundedAt           *time.Time `json:"refunded_at"`
	FailureReason        string     `json:"failure_reason"`
}

// adminPaymentEventSummary intentionally excludes the provider callback
// payload. Operators need the processing timeline and external references,
// not a second raw webhook viewer with uncontrolled encoding or secrets.
type adminPaymentEventSummary struct {
	ID              uint       `json:"id"`
	Provider        string     `json:"provider"`
	ProviderEventID string     `json:"provider_event_id"`
	EventType       string     `json:"event_type"`
	AmountMinor     int64      `json:"amount_minor"`
	SignatureValid  bool       `json:"signature_valid"`
	ProcessedAt     *time.Time `json:"processed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type adminUserDetail struct {
	ID                      uint       `json:"id"`
	AccountName             string     `json:"account_name"`
	Email                   string     `json:"email"`
	EmailVerifiedAt         *time.Time `json:"email_verified_at"`
	LastLoginAt             *time.Time `json:"last_login_at"`
	IsAdmin                 bool       `json:"is_admin"`
	Status                  string     `json:"status"`
	ActiveSubscriptionCount int64      `json:"active_subscription_count"`
	TotalSubscriptionCount  int64      `json:"total_subscription_count"`
	PendingOrderCount       int64      `json:"pending_order_count"`
	TotalOrderCount         int64      `json:"total_order_count"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type adminSubscriptionDetail struct {
	adminSubscriptionListItem
	ActiveCredentialCount int64 `json:"active_credential_count"`
	TotalCredentialCount  int64 `json:"total_credential_count"`
}

func newAdminOrderListItem(order model.Order) adminOrderListItem {
	return adminOrderListItem{
		ID: order.ID, UserID: order.UserID, SubscriptionID: order.SubscriptionID,
		PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, TradeNo: order.TradeNo,
		OrderType: order.OrderType, AmountCents: order.AmountCents, Currency: order.Currency,
		Status: order.Status, PlanName: order.PlanName, SKUName: order.SKUName,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

func newAdminOrderDetail(order model.Order) adminOrderDetail {
	return adminOrderDetail{
		adminOrderListItem:   newAdminOrderListItem(order),
		TargetSubscriptionID: order.TargetSubscriptionID,
		PayableAmount:        order.PayableAmount,
		PaidAmount:           order.PaidAmount,
		RefundAmount:         order.RefundAmount,
		DiscountAmount:       order.DiscountAmount,
		Channel:              order.Channel,
		ProviderTradeNo:      order.ProviderTradeNo,
		BillingUnit:          order.BillingUnit,
		BillingValue:         order.BillingValue,
		RenewalEffect:        order.RenewalEffect,
		TrafficBytes:         order.TrafficBytes,
		DeviceLimit:          order.DeviceLimit,
		SpeedLimitMbps:       order.SpeedLimitMbps,
		PaidAt:               order.PaidAt,
		CanceledAt:           order.CanceledAt,
		FulfilledAt:          order.FulfilledAt,
		RefundedAt:           order.RefundedAt,
		FailureReason:        order.FailureReason,
	}
}

func newAdminOrderList(orders []model.Order) []adminOrderListItem {
	items := make([]adminOrderListItem, 0, len(orders))
	for _, order := range orders {
		items = append(items, newAdminOrderListItem(order))
	}
	return items
}

func (h *handlers) AdminUserGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var user model.User
	if err := h.db.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}

	detail := adminUserDetail{
		ID: user.ID, AccountName: user.AccountName, Email: user.Email,
		EmailVerifiedAt: user.EmailVerifiedAt, LastLoginAt: user.LastLoginAt,
		IsAdmin: user.IsAdmin, Status: user.Status,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
	now := time.Now().UTC()
	counts := []struct {
		target *int64
		query  *gorm.DB
	}{
		{&detail.ActiveSubscriptionCount, h.db.Model(&model.Subscription{}).Where(
			"user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total",
			id, subStatusActive, now,
		)},
		{&detail.TotalSubscriptionCount, h.db.Model(&model.Subscription{}).Where("user_id = ?", id)},
		{&detail.PendingOrderCount, h.db.Model(&model.Order{}).Where("user_id = ? AND status = ?", id, orderStatusPending)},
		{&detail.TotalOrderCount, h.db.Model(&model.Order{}).Where("user_id = ?", id)},
	}
	for _, count := range counts {
		if err := count.query.Count(count.target).Error; err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, detail)
}

func (h *handlers) AdminOrderGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var order model.Order
	if err := h.db.First(&order, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, newAdminOrderDetail(order))
}

func (h *handlers) AdminOrderPaymentEventsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	orderID, err := parsePathID(r.URL.Path, "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var orderExists int64
	if err := h.db.Model(&model.Order{}).Where("id = ?", orderID).Count(&orderExists).Error; err != nil {
		ServerError(w, err)
		return
	}
	if orderExists == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.PaymentEvent{}).Where("order_id = ?", orderID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]adminPaymentEventSummary, 0)
	if err := query.
		Select("id, provider, provider_event_id, event_type, amount_minor, signature_valid, processed_at, created_at").
		Order("id desc").
		Offset(offset).
		Limit(limit).
		Scan(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) AdminSubscriptionGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscriptions/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	var item adminSubscriptionListItem
	now := time.Now().UTC()
	result := h.db.Table("subscriptions").
		Select(`subscriptions.id, subscriptions.user_id, users.email AS user_email,
			subscriptions.plan_id, plans.name AS plan_name,
			subscriptions.plan_sku_id, plan_skus.name AS sku_name,
			subscriptions.node_group_id, subscriptions.subscription_type,
			subscriptions.start_at, subscriptions.end_at,
			CASE
				WHEN subscriptions.status = 'active'
					AND (subscriptions.end_at <= ? OR subscriptions.flow_used >= subscriptions.flow_total)
				THEN 'expired'
				ELSE subscriptions.status
			END AS status,
			subscriptions.flow_total, subscriptions.flow_used,
			subscriptions.speed_limit_mbps, subscriptions.device_limit,
			subscriptions.family_limit, subscriptions.renewal_price_minor,
			subscriptions.reset_policy, subscriptions.next_reset_at,
			subscriptions.traffic_calc_mode, subscriptions.created_at,
			subscriptions.updated_at`, now).
		Joins("LEFT JOIN users ON users.id = subscriptions.user_id").
		Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").
		Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
		Where("subscriptions.id = ?", id).Scan(&item)
	if result.Error != nil {
		ServerError(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		NotFound(w)
		return
	}

	detail := adminSubscriptionDetail{adminSubscriptionListItem: item}
	if err := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", id).Count(&detail.TotalCredentialCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status = ?", id, protocolCredentialStatusActive).Count(&detail.ActiveCredentialCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, detail)
}
