package handler

import (
	"encoding/json"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func (h *handlers) pluginIdentity(w http.ResponseWriter, r *http.Request) (authClaims, bool) {
	if r.Header.Get("Authorization") == "" {
		return authClaims{}, true
	}
	c, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return c, false
	}
	return c, true
}
func (h *handlers) PluginCatalogHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	pages, err := h.pluginManager.Pages(r.URL.Query().Get("surface"), c.UserID, c.IsAdmin)
	if err != nil {
		Forbidden(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, pages)
}
func (h *handlers) PluginSlotCatalogHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	slots, err := h.pluginManager.Slots(r.URL.Query().Get("surface"), r.URL.Query().Get("slot"), c.UserID, c.IsAdmin)
	if err != nil {
		Forbidden(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, slots)
}
func (h *handlers) PluginSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		Page          string `json:"page"`
		Surface       string `json:"surface"`
		Configuration bool   `json:"configuration"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	s, err := h.pluginManager.CreateSession(pathvar.Vars(r)["id"], body.Page, body.Surface, c.UserID, c.IsAdmin, body.Configuration)
	if err != nil {
		Forbidden(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, s)
}
func (h *handlers) PluginSlotSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		ID           string `json:"id"`
		Slot         string `json:"slot"`
		Surface      string `json:"surface"`
		TargetUserID uint   `json:"target_user_id"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	if body.Surface == "account" {
		body.TargetUserID = c.UserID
	}
	if body.Surface == "admin" {
		var count int64
		if !c.IsAdmin || body.TargetUserID == 0 || h.db.Model(&model.User{}).Where("id = ?", body.TargetUserID).Count(&count).Error != nil || count != 1 {
			Forbidden(w, "plugin slot target is not authorized")
			return
		}
	}
	s, err := h.pluginManager.CreateSlotSession(pathvar.Vars(r)["id"], body.ID, body.Slot, body.Surface, c.UserID, c.IsAdmin, body.TargetUserID)
	if err != nil {
		Forbidden(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, s)
}
func (h *handlers) PluginBridgeHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	s, err := h.pluginManager.CheckSession(r.Header.Get("X-Plugin-Session"), c.UserID, c.IsAdmin)
	if err != nil {
		Forbidden(w, "invalid plugin session")
		return
	}
	var body struct {
		Type       string          `json:"type"`
		Revision   uint64          `json:"revision"`
		Config     json.RawMessage `json:"config"`
		Key        string          `json:"key"`
		Value      json.RawMessage `json:"value"`
		ProviderID string          `json:"provider_id"`
		IdentityID string          `json:"identity_id"`
		Password   string          `json:"password"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if body.Type == "context.load" {
		OK(w, map[string]any{"surface": s.Surface, "plugin_id": s.PluginID, "page_id": s.PageID, "slot_id": s.SlotID, "slot": s.Slot, "target_user_id": s.TargetUserID})
		return
	}
	if strings.HasPrefix(body.Type, "identity.") {
		if s.Purpose != "slot" {
			Forbidden(w, "capability not granted")
			return
		}
		switch body.Type {
		case "identity.providers.list":
			providers, err := h.pluginIdentityProviderViews(s.PluginID)
			if err != nil {
				pluginError(w, err)
				return
			}
			OK(w, providers)
		case "identity.bindings.list":
			if s.Surface != "account" && s.Surface != "admin" {
				Forbidden(w, "capability not granted")
				return
			}
			bindings, err := h.pluginIdentityBindings(s.TargetUserID, s.PluginID)
			if err != nil {
				pluginError(w, err)
				return
			}
			OK(w, bindings)
		case "identity.login.start", "identity.bind.start":
			bind := body.Type == "identity.bind.start"
			if bind && s.Surface != "account" || !bind && s.Surface != "public" || !pluginOwnsProvider(s.PluginID, body.ProviderID) {
				Forbidden(w, "capability not granted")
				return
			}
			status, message, authorization := h.beginExternalAuth(w, r, bind, body.ProviderID, body.Password)
			if status != 0 {
				writeJSON(w, status, message, nil)
				return
			}
			OK(w, map[string]string{"authorization_url": authorization})
		case "identity.binding.unlink":
			if err := h.unlinkPluginIdentity(s, c, body.IdentityID, body.Password); err != nil {
				pluginError(w, err)
				return
			}
			OK(w, map[string]bool{"unlinked": true})
		default:
			Forbidden(w, "unsupported plugin capability")
		}
		return
	}
	if strings.HasPrefix(body.Type, "storage.") {
		result, err := h.pluginManager.SessionStorage(r.Header.Get("X-Plugin-Session"), c.UserID, c.IsAdmin, plugins.StorageRequest{Type: body.Type, Key: body.Key, Revision: body.Revision, Value: body.Value})
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, result)
		return
	}
	if s.Purpose != "configuration" || !c.IsAdmin {
		Forbidden(w, "capability not granted")
		return
	}
	switch body.Type {
	case "config.load":
		view, err := h.pluginManager.Config(s.PluginID)
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, view)
	case "config.save":
		view, err := h.pluginManager.SaveSessionConfig(r.Context(), r.Header.Get("X-Plugin-Session"), c.UserID, c.IsAdmin, c.Email, body.Revision, body.Config)
		if err != nil {
			pluginError(w, err)
			return
		}
		OK(w, view)
	case "config.test":
		if err := h.pluginManager.TestSessionConfig(r.Context(), r.Header.Get("X-Plugin-Session"), c.UserID, c.IsAdmin, c.Email); err != nil {
			pluginError(w, err)
			return
		}
		OK(w, map[string]bool{"healthy": true})
	default:
		Forbidden(w, "unsupported plugin capability")
	}
}
func (h *handlers) PluginAssetHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/plugin-assets/"), "/", 2)
	if len(parts) != 2 || len(parts[0]) != 64 {
		http.NotFound(w, r)
		return
	}
	data, err := h.pluginManager.Asset(parts[0], parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	scope := "http://" + r.Host + "/api/v1/plugin-assets/" + parts[0] + "/ https://" + r.Host + "/api/v1/plugin-assets/" + parts[0] + "/"
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline' "+scope+"; style-src 'unsafe-inline' "+scope+"; img-src data: "+scope+"; font-src "+scope+"; connect-src 'none'; form-action 'none'; frame-src 'none'; base-uri 'none'; frame-ancestors 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	kind := mime.TypeByExtension(path.Ext(parts[1]))
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	_, _ = w.Write(data)
}

func (h *handlers) PluginRevokeSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	h.pluginManager.RevokeSession(r.Header.Get("X-Plugin-Session"), c.UserID)
	w.Header().Set("Cache-Control", "no-store")
	OK(w, map[string]bool{"revoked": true})
}
