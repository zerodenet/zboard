package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type adminOrderAssignmentRequest struct {
	PayableAmount        *int64 `json:"payable_amount"`
	UserID               uint   `json:"user_id"`
	PlanSKUID            uint   `json:"plan_sku_id"`
	TargetSubscriptionID uint   `json:"target_subscription_id"`
	Note                 string `json:"note"`
	RequestID            string `json:"request_id"`
}

func (h *handlers) AdminOrderAssignHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request adminOrderAssignmentRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.Note = strings.TrimSpace(request.Note)
	requestID, requestIDError := uuid.Parse(request.RequestID)
	fields := map[string]string{}
	if request.UserID == 0 {
		fields["user_id"] = "请选择用户。"
	}
	if request.PlanSKUID == 0 {
		fields["plan_sku_id"] = "请选择销售规格。"
	}
	if request.PayableAmount != nil && (*request.PayableAmount < 0 || *request.PayableAmount > 9007199254740991) {
		fields["payable_amount"] = "应付金额必须为不小于 0 的整数分，且不能超过安全整数范围。"
	}
	if request.Note == "" || utf8.RuneCountInString(request.Note) > 500 {
		fields["note"] = "请填写分配原因，最多 500 字。"
	}
	if requestIDError != nil || requestID == uuid.Nil {
		fields["request_id"] = "缺少有效的操作标识，请重新打开分配窗口。"
	}
	if len(fields) != 0 {
		BadRequestFields(w, "订单分配失败。", fields)
		return
	}
	request.RequestID = requestID.String()
	payload, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(payload))
	// The existing unique trade number also fences concurrent retries. Scope the
	// operation to its administrator; payload changes must use a new request ID.
	tradeNo := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("admin-assign:%d:%s", actor.UserID, request.RequestID))))
	var order model.Order
	err = h.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := lockOrderSettlementUsers(tx, request.UserID, actor.UserID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validationError("订单分配失败。", map[string]string{"user_id": "用户不存在。"})
			}
			return err
		}
		err := tx.Where("trade_no = ?", tradeNo).First(&order).Error
		if err == nil {
			if order.AssignmentFingerprint != fingerprint {
				return validationError("本次操作标识已用于另一笔分配，请重新打开分配窗口。", nil)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var user model.User
		if err := tx.First(&user, request.UserID).Error; err != nil {
			return err
		}
		if user.Status != "active" {
			return validationError("订单分配失败。", map[string]string{"user_id": "请先启用该用户。"})
		}
		if err := h.createCommerceOrder(tx, request.UserID, commerceOrderCreateRequest{
			PlanSKUID: request.PlanSKUID, TargetSubscriptionID: request.TargetSubscriptionID, Channel: "admin_assignment",
		}, &order); err != nil {
			return err
		}
		if request.PayableAmount != nil {
			order.PayableAmount = *request.PayableAmount
			order.DiscountAmount = max(order.AmountCents-order.PayableAmount, 0)
		}
		order.TradeNo = tradeNo
		order.AssignedBy = actor.UserID
		order.AssignmentNote = request.Note
		order.AssignmentFingerprint = fingerprint
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		return createAuditLog(tx, actor, "order.assign", fmt.Sprintf("order:%d", order.ID), fmt.Sprintf("user:%d price:%d payable:%d currency:%s reason:%s", request.UserID, order.AmountCents, order.PayableAmount, order.Currency, request.Note))
	})
	if errors.Is(err, errPlanSubscriptionLimitReached) {
		writePlanSubscriptionLimitReached(w)
		return
	}
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
		} else {
			writeCommercePersistenceFailure(w, "订单分配失败，请稍后重试。")
		}
		return
	}
	OK(w, newAdminOrderDetail(order))
}
