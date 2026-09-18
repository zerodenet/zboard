package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net/http"
	"time"
)

type maintenanceState = platform.MaintenanceState
type maintenanceUpdateRequest = platform.MaintenanceUpdate

func maintenanceRouteAllowedWithoutAdmin(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/api/v1/version", "/api/v1/setup/status", "/api/v1/system/status", "/api/v1/system/configs",
		"/api/v1/announcements", "/api/v1/auth/login", "/api/v1/auth/me":
		return true
	default:
		return false
	}
}

func (h *handlers) invalidateMaintenanceState() { h.services.InvalidateMaintenance() }
func (h *handlers) backgroundWorkPaused() bool  { return h.services.WorkPaused() }
func (h *handlers) loadMaintenanceState(force bool) (maintenanceState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.services.MaintenanceState(ctx, force)
}

func (h *handlers) SystemStatusHandler(w http.ResponseWriter, r *http.Request) {
	state, err := h.loadMaintenanceState(true)
	if err != nil {
		ServerError(w, err)
		return
	}
	announcements, err := h.activeAnnouncements(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	unread, err := h.announcementUnreadCount(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"maintenance": state, "announcements": announcements, "announcement_unread_count": unread, "as_of": time.Now().UTC()})
}

func (h *handlers) AdminMaintenanceUpdateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req maintenanceUpdateRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	err = h.services.Maintenance.Update(r.Context(), claims.UserID, req)
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
	state, err := h.loadMaintenanceState(true)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, state)
}
