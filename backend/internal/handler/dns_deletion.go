package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
)

func (h *handlers) ManagedDNSDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/dns-records/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.ResourceRemoval.DNS(r.Context(), claims.UserID, id)
	if err != nil {
		resourceRemovalError(w, err)
		return
	}
	OK(w, result)
}
func resourceRemovalError(w http.ResponseWriter, err error) {
	var blocked *network.ResourceRemovalBlocked
	var dependencies *network.DeletionBlocked
	switch {
	case errors.Is(err, network.ErrResourcesBusy):
		writeJSON(w, http.StatusConflict, "相关资源正在更新，请重新检查删除依赖后重试。", nil)
	case errors.As(err, &dependencies):
		writeJSON(w, http.StatusConflict, dependencies.Error(), dependencies.Check)
	case errors.As(err, &blocked):
		writeJSON(w, http.StatusConflict, "请先完成或核验资源任务。", map[string]any{"blockers": blocked.Blockers})
	case errors.Is(err, network.ErrResourceNotFound):
		NotFound(w)
	case errors.Is(err, network.ErrResourcePermission):
		writeJSON(w, http.StatusForbidden, "需要管理员权限。", nil)
	default:
		ServerError(w, err)
	}
}
