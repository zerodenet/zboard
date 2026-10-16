package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"gorm.io/gorm"
)

func (h *handlers) SetPluginManager(m *plugins.Manager) {
	h.pluginManager = m
	h.identityProviders = nil
	if m != nil {
		h.identityProviders = m
		m.SetTaskPauseCheck(h.services.WorkPaused)
		h.backgroundJobs()
		m.SetTaskRuntime(h.services.Jobs)
	}
}
func pluginError(w http.ResponseWriter, err error) {
	var failure *pluginv1.ActionError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		writeCapabilityError(w, http.StatusGatewayTimeout, "plugin request timed out", "deadline_exceeded")
	case errors.Is(err, plugins.ErrRateLimited):
		writeCapabilityError(w, http.StatusTooManyRequests, err.Error(), "rate_limited")
	case errors.Is(err, plugins.ErrInvalidSession):
		writeCapabilityError(w, http.StatusForbidden, err.Error(), "session_expired")
	case errors.Is(err, plugins.ErrPermission):
		writeCapabilityError(w, http.StatusForbidden, err.Error(), "permission_denied")
	case errors.Is(err, gorm.ErrRecordNotFound):
		NotFound(w)
	case errors.Is(err, plugins.ErrConflict):
		writeCapabilityError(w, http.StatusConflict, err.Error(), "conflict")
	case errors.Is(err, plugins.ErrUnavailable):
		writeCapabilityError(w, http.StatusServiceUnavailable, err.Error(), "unavailable")
	case errors.As(err, &failure):
		// Revalidate even local errors; only explicitly public, bounded values
		// may leave the host. The plugin cannot select the HTTP/host error code.
		value, ok := pluginv1.ParseActionError(failure)
		if !ok {
			writeCapabilityError(w, http.StatusServiceUnavailable, "plugin service unavailable", "unavailable")
			return
		}
		writeJSONResponse(w, http.StatusUnprocessableEntity, value.Message, nil, &APIError{Version: 1, Code: "plugin_action_rejected", Source: "plugin", PluginCode: value.Code, Retryable: &value.Retryable})
	default:
		writeCapabilityError(w, http.StatusBadRequest, err.Error(), "invalid_argument")
	}
}
func pluginBody(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, plugins.MaxConfigBytes+4096))
	if err == nil {
		err = plugins.DecodeStrict(raw, out)
	}
	if err != nil {
		writeCapabilityError(w, http.StatusBadRequest, "invalid or oversized plugin request", "invalid_argument")
		return false
	}
	return true
}
func (h *handlers) AdminPluginsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if h.pluginManager == nil {
		pluginError(w, plugins.ErrUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		items, err := h.pluginManager.List()
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, items)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, plugins.MaxPackageBytes))
	if err != nil {
		BadRequest(w, "plugin package exceeds 32 MiB")
		return
	}
	publicKey := r.Header.Get("X-Plugin-Public-Key")
	if r.URL.Query().Get("inspect") == "true" {
		preview, err := h.pluginManager.PreviewImport(raw, publicKey)
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, preview)
		return
	}
	item, err := h.pluginManager.ImportConfirmed(raw, claims.Email, publicKey, r.Header.Get("X-Plugin-Digest"), r.Header.Get("X-Plugin-Trust-Fingerprint"), r.Header.Get("X-Plugin-Publisher-Migration"))
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, item)
}
func (h *handlers) AdminPluginActionHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body struct {
		Generation     uint64 `json:"generation"`
		AcceptUntested bool   `json:"accept_untested"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	vars := pathvar.Vars(r)
	item, err := h.pluginManager.Action(r.Context(), vars["id"], vars["action"], claims.Email, body.Generation, body.AcceptUntested, "")
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, item)
}
func (h *handlers) AdminPluginConfigHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id := pathvar.Vars(r)["id"]
	if r.Method == http.MethodGet {
		view, err := h.pluginManager.Config(id)
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, view)
		return
	}
	var body struct {
		Revision uint64          `json:"revision"`
		Config   json.RawMessage `json:"config"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	view, err := h.pluginManager.SaveConfig(r.Context(), id, claims.Email, body.Revision, body.Config)
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, view)
}
func (h *handlers) AdminPluginTestHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if err := h.pluginManager.TestConfig(r.Context(), pathvar.Vars(r)["id"], claims.Email); err != nil {
		pluginError(w, err)
		return
	}
	OK(w, map[string]any{"healthy": true})
}
func (h *handlers) AdminPluginOperationsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	items, err := h.pluginManager.Operations(pathvar.Vars(r)["id"])
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, items)
}
func (h *handlers) AdminPluginMarketHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodGet {
		market, err := h.pluginManager.Market(r.Context())
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, market)
		return
	}
	var body struct {
		ID     string `json:"id"`
		Digest string `json:"digest"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	item, err := h.pluginManager.InstallMarket(r.Context(), body.ID, body.Digest, claims.Email)
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, item)
}

// Plugin failures cannot take the core console offline.
func (h *handlers) PluginGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.pluginManager == nil {
			pluginError(w, plugins.ErrUnavailable)
			return
		}
		next(w, r)
	}
}
