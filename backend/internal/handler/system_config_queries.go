package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net/http"
)

func (h *handlers) PublicSystemConfigsHandler(w http.ResponseWriter, r *http.Request) {
	views, err := h.services.Settings.Public(r.Context())
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, views)
}
func (h *handlers) AdminSystemConfigsListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	views, err := h.services.Settings.Administrative(r.Context(), claims.UserID)
	if errors.Is(err, platform.ErrSettingsPermission) {
		Forbidden(w, "settings administrator authorization changed")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, views)
}
