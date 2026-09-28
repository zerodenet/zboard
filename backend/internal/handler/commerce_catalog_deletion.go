package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanDeleteCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.deleteCatalogEntry(w, r, true)
}
func (h *handlers) PlanSKUDeleteCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.deleteCatalogEntry(w, r, false)
}
func (h *handlers) deleteCatalogEntry(w http.ResponseWriter, r *http.Request, plan bool) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	prefix := "/api/v1/admin/plan-skus/"
	if plan {
		prefix = "/api/v1/admin/plans/"
	}
	id, err := parsePathID(r.URL.Path, prefix)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if plan {
		err = h.services.CatalogDeletion.DeletePlan(r.Context(), actor.UserID, id)
	} else {
		err = h.services.CatalogDeletion.DeleteSKU(r.Context(), actor.UserID, id)
	}
	switch {
	case errors.Is(err, commerce.ErrPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	case err != nil:
		writeCommercePersistenceFailure(w, "删除失败，请稍后重试。")
	default:
		OK(w, map[string]any{"id": id})
	}
}
