package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func (h *handlers) PlanSKUCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.planSKUCreateCommerceHandler(w, r)
}

func (h *handlers) planSKUCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commercePlanSKURequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	value, err := h.services.SKUCreation.Create(r.Context(), claims.UserID, planID, request)
	if err != nil {
		switch {
		case errors.Is(err, commerce.ErrPermission):
			Forbidden(w, "需要当前有效的管理员权限。")
		case errors.Is(err, commerce.ErrNotFound):
			NotFound(w)
		case errors.Is(err, commerce.ErrSKUCodeConflict):
			writePlanSKUCodeConflict(w, "code")
		default:
			var invalid *commerce.ValidationError
			if errors.As(err, &invalid) {
				BadRequestError(w, commerceValidationError(err))
			} else {
				writeCommercePersistenceFailure(w, "销售规格保存失败，请稍后重试。")
			}
		}
		return
	}
	OK(w, value)
}
