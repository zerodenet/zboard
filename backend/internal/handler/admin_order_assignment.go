package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

type adminOrderAssignmentRequest = commerce.OrderAssignmentRequest

func (h *handlers) AdminOrderAssignHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request commerce.OrderAssignmentRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	order, err := h.services.OrderAssignment.Assign(r.Context(), actor.UserID, request)
	if err != nil {
		var invalid *commerce.ValidationError
		switch {
		case errors.Is(err, commerce.ErrOrderPermission):
			Forbidden(w, "需要当前有效的管理员权限。")
		case errors.Is(err, commerce.ErrSubscriptionCapacity):
			writePlanSubscriptionLimitReached(w)
		case errors.As(err, &invalid):
			BadRequestError(w, commerceValidationError(err))
		default:
			writeCommercePersistenceFailure(w, "订单分配失败，请稍后重试。")
		}
		return
	}
	OK(w, commerce.DetailOrder(order))
}
