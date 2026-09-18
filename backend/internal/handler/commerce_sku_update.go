package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanSKUUpdateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.planSKUUpdateCommerceHandler(w, r)
}

func (h *handlers) planSKUUpdateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commercePlanSKURequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.SKUUpdate.Update(r.Context(), claims.UserID, id, request)
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
	OK(w, view)
}
