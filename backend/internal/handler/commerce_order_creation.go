package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func (h *handlers) OrderCreateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	var request commerce.OrderCreateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	order, err := h.services.OrderCreation.Create(r.Context(), claims.UserID, request)
	if err != nil {
		var invalid *commerce.ValidationError
		switch {
		case errors.Is(err, commerce.ErrSubscriptionQuoteChanged):
			writeCapabilityError(w, http.StatusConflict, "订阅权益或优惠依据已变化，请核对原订单后重新确认报价。", "subscription_quote_changed")
		case errors.Is(err, commerce.ErrDiscountConfirmationRequired):
			writeCapabilityError(w, http.StatusConflict, "优惠协议需要重新确认，请先核对原订单后重新获取报价。", "discount_confirmation_required")
		case errors.Is(err, commerce.ErrDiscountQuoteUnavailable):
			writeCapabilityError(w, http.StatusServiceUnavailable, "优惠当前不可用，请重新确认报价。", "discount_unavailable")
		case errors.Is(err, commerce.ErrDiscountQuoteInvalid):
			writeCapabilityError(w, http.StatusBadGateway, "优惠报价未通过校验，请重新确认报价。", "discount_invalid")
		case errors.Is(err, commerce.ErrBuyerUnavailable):
			Forbidden(w, "当前账号不可创建订单。")
		case errors.Is(err, commerce.ErrSubscriptionCapacity):
			writePlanSubscriptionLimitReached(w)
		case errors.As(err, &invalid):
			BadRequestError(w, commerceValidationError(err))
		default:
			writeCommercePersistenceFailure(w, "订单创建失败，请稍后重试。")
		}
		return
	}
	OK(w, order)
}

func (h *handlers) OrderPreviewHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	var request commerce.OrderCreateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	preview, err := h.services.OrderCreation.Preview(r.Context(), claims.UserID, request)
	if err != nil {
		var invalid *commerce.ValidationError
		switch {
		case errors.Is(err, commerce.ErrSubscriptionQuoteChanged):
			writeCapabilityError(w, http.StatusConflict, "订阅权益或优惠依据已变化，请核对原订单后重新确认报价。", "subscription_quote_changed")
		case errors.Is(err, commerce.ErrDiscountConfirmationRequired):
			writeCapabilityError(w, http.StatusConflict, "优惠协议需要重新确认，请先核对原订单后重新获取报价。", "discount_confirmation_required")
		case errors.Is(err, commerce.ErrDiscountQuoteUnavailable):
			writeCapabilityError(w, http.StatusServiceUnavailable, "优惠当前不可用，请重新确认报价。", "discount_unavailable")
		case errors.Is(err, commerce.ErrDiscountQuoteInvalid):
			writeCapabilityError(w, http.StatusBadGateway, "优惠报价未通过校验，请重新确认报价。", "discount_invalid")
		case errors.Is(err, commerce.ErrBuyerUnavailable):
			Forbidden(w, "当前账号不可创建订单。")
		case errors.Is(err, commerce.ErrSubscriptionCapacity):
			writePlanSubscriptionLimitReached(w)
		case errors.As(err, &invalid):
			BadRequestError(w, commerceValidationError(err))
		default:
			writeCommercePersistenceFailure(w, "报价失败，请稍后重试。")
		}
		return
	}
	OK(w, preview)
}
