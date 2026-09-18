package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.planCreateCommerceHandler(w, r)
}

func (h *handlers) planCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request commerce.PlanCreateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.PlanCreation.Create(r.Context(), claims.UserID, request)
	if err != nil {
		var conflict *commerce.IdentifierConflict
		var invalid *commerce.ValidationError
		switch {
		case errors.Is(err, commerce.ErrPermission):
			Forbidden(w, "需要当前有效的管理员权限。")
		case errors.As(err, &conflict):
			writeCommerceError(w, http.StatusBadRequest, commerceErrorIdentifierConflict, conflict.Error(), conflict.Fields)
		case errors.As(err, &invalid):
			BadRequestError(w, commerceValidationError(err))
		default:
			writeCommercePersistenceFailure(w, "商品数据保存失败，请稍后重试。")
		}
		return
	}
	OK(w, view)
}
