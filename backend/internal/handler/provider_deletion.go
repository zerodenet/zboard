package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
)

func (h *handlers) ProviderAccountDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/provider-accounts/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.ProviderDirectory.Delete(r.Context(), claims.UserID, id)
	if err != nil {
		var blocked *network.ProviderDeletionBlocked
		if errors.As(err, &blocked) {
			writeJSON(w, http.StatusConflict, "请先完成或核验供应商及证书任务。", map[string]any{"blockers": blocked.Blockers})
		} else {
			providerAccountError(w, err)
		}
		return
	}
	OK(w, result)
}
