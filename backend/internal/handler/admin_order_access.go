package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
	"strings"
)

func (h *handlers) AdminOrderSubscriptionAccessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/subscription-access") {
		BadRequest(w, "invalid order access path")
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/subscription-access"), "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.OrderAccess(h.credentialCipher).Read(r.Context(), actor.UserID, id)
	switch {
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	case errors.Is(err, commerce.ErrOrderPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	case err != nil:
		writeCommercePersistenceFailure(w, "订阅地址读取失败，请稍后重试。")
	default:
		completeAccessURL(&out)
		out.Token = ""
		OK(w, out)
	}
}
