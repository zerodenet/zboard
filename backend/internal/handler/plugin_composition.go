package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"net/http"
)

func (h *handlers) PluginCompositionHandler(w http.ResponseWriter, r *http.Request) {
	surface := r.URL.Query().Get("surface")
	c := authClaims{}
	if surface == "admin" {
		var err error
		c, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else if surface != "public" {
		var ok bool
		c, ok = h.pluginIdentity(w, r)
		if !ok {
			return
		}
		if c.UserID == 0 {
			Unauthorized(w, "authentication required")
			return
		}
		account, err := h.services.Identity.Accounts.Me(r.Context(), identity.Principal{ID: c.UserID})
		if err != nil {
			Forbidden(w, "account unavailable")
			return
		}
		c.IsAdmin = account.IsAdmin
	}
	out, err := h.pluginManager.UIComposition(r.Context(), surface, c.UserID, c.IsAdmin)
	if err != nil {
		pluginError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}

// A native login button carries no plugin token. Begin still checks current
// admission, declared public slot, provider ownership, origin and OAuth state.
func (h *handlers) PluginIdentityStartHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SlotID         string `json:"slot_id"`
		ProviderID     string `json:"provider_id"`
		Generation     uint64 `json:"generation"`
		ConfigRevision uint64 `json:"config_revision"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	id := pathvar.Vars(r)["id"]
	slots, err := h.pluginManager.Slots("public", "auth.login.methods", 0, false)
	if err != nil {
		pluginError(w, err)
		return
	}
	allowed := false
	for _, v := range slots {
		if v.PluginID == id && v.Slot.ID == body.SlotID && pluginOwnsProvider(id, body.ProviderID) {
			allowed = true
			break
		}
	}
	if !allowed {
		pluginError(w, plugins.ErrPermission)
		return
	}
	authNoStore(w)
	code, message, target := h.beginExternalAuthExpected(w, r, false, body.ProviderID, "", &identityPresentationFence{Generation: body.Generation, Revision: body.ConfigRevision})
	if code != 0 {
		writeJSON(w, code, message, nil)
		return
	}
	OK(w, map[string]string{"authorization_url": target})
}
