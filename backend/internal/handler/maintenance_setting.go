package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net/http"
)

// The legacy settings route keeps its existing projection and value encoding;
// platform owns authorization, mutation and migration exclusion.
func (h *handlers) updateMaintenanceSetting(w http.ResponseWriter, r *http.Request, claims authClaims, key string, req systemConfigUpdateReq) {
	config, err := h.services.Settings.Get(r.Context(), key)
	if err != nil {
		if errors.Is(err, platform.ErrSettingNotFound) {
			NotFound(w)
		} else {
			ServerError(w, err)
		}
		return
	}
	value, err := platform.NormalizeSettingValue(config, req.Value)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	err = h.services.Maintenance.Patch(r.Context(), claims.UserID, platform.MaintenanceSetting{Key: key, Value: value, ExpectedRevision: req.ExpectedRevision})
	if errors.Is(err, platform.ErrMaintenancePermission) {
		Forbidden(w, "maintenance administrator authorization changed")
		return
	}
	if errors.Is(err, platform.ErrMaintenanceRevision) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.invalidateMaintenanceState()
	view, err := h.services.Settings.View(r.Context(), key)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, view)
}
