package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanUpdateHandler(w http.ResponseWriter, r *http.Request) {
	h.planUpdateHandler(w, r)
}

func (h *handlers) planUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commerce.PlanUpdateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.PlanUpdate.Update(r.Context(), claims.UserID, id, request)
	if err != nil {
		var revision *commerce.PlanRevisionError
		var invalid *commerce.ValidationError
		switch {
		case errors.Is(err, commerce.ErrPermission):
			Forbidden(w, "需要当前有效的管理员权限。")
		case errors.Is(err, commerce.ErrNotFound):
			NotFound(w)
		case errors.Is(err, commerce.ErrPlanNameConflict):
			writeCommerceError(w, http.StatusBadRequest, "plan_name_conflict", "商品名称已被使用。", map[string]string{"name": "该商品名称已被使用，请更换后重试。"})
		case errors.Is(err, commerce.ErrPlanSlugConflict):
			writePlanSlugConflict(w)
		case errors.As(err, &revision):
			status, message := http.StatusConflict, "商品已被其他会话更新，请重新加载最新版本。"
			if revision.Required {
				status, message = http.StatusPreconditionRequired, "保存商品前需要提供当前版本号。"
			}
			writeJSON(w, status, message, map[string]interface{}{"current_revision": revision.Current})
		case errors.As(err, &invalid):
			BadRequestError(w, commerceValidationError(err))
		default:
			writeCommercePersistenceFailure(w, "商品数据保存失败，请稍后重试。")
		}
		return
	}
	OK(w, view)
}
