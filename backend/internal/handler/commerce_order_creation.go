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
