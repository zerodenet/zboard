package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/navigation"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func (h *handlers) NavigationHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := h.pluginIdentity(w, r)
	if !ok {
		return
	}
	surface := r.URL.Query().Get("surface")
	if !navigation.SurfaceValid(surface) {
		BadRequest(w, "unknown surface")
		return
	}
	if surface == "admin" && !c.IsAdmin || surface == "account" && c.UserID == 0 {
		Forbidden(w, "surface not authorized")
		return
	}
	snapshot, err := h.services.Navigation.Read(r.Context(), surface)
	if err != nil {
		ServerError(w, err)
		return
	}
	pagePath := r.URL.Query().Get("path")
	if pagePath != "" && (!navigation.ValidPath(pagePath) || len(pagePath) > 512) {
		BadRequest(w, "invalid page path")
		return
	}
	all := snapshot.Nodes
	allowed := map[string]bool{}
	if h.pluginManager != nil {
		pages, pageErr := h.pluginManager.Pages(surface, c.UserID, c.IsAdmin)
		if pageErr == nil {
			for _, p := range pages {
				allowed[navigation.PluginNodeID(p.PluginID, surface, p.Page.ID)] = true
			}
		}
	}
	documents := false
	if surface == "public" {
		documents, err = h.services.Navigation.HasDocuments(r.Context())
		if err != nil {
			ServerError(w, err)
			return
		}
	}
	snapshot.Nodes = navigation.Visible(snapshot.Nodes, func(n navigation.Node) bool {
		if n.Owner == "plugin" && !allowed[n.ID] {
			return false
		}
		if n.Condition == "admin" {
			return c.IsAdmin
		}
		if n.Condition == "documents" {
			return documents
		}
		return true
	})
	visible := snapshot.Nodes
	// A shortcut must not continue advertising a registered page that is hidden.
	snapshot.Nodes = navigation.Visible(visible, func(n navigation.Node) bool {
		return n.Owner != "custom" || n.Path == "" || navigation.PageAvailable(all, visible, n.Path)
	})
	w.Header().Set("Cache-Control", "no-store")
	var available *bool
	if pagePath != "" {
		value := navigation.PageAvailable(all, snapshot.Nodes, pagePath)
		available = &value
	}
	OK(w, struct {
		navigation.Snapshot
		PageAvailable *bool `json:"page_available,omitempty"`
	}{snapshot, available})
}

func (h *handlers) AdminMenusHandler(w http.ResponseWriter, r *http.Request) {
	c, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	surface := r.URL.Query().Get("surface")
	if !navigation.SurfaceValid(surface) {
		BadRequest(w, "unknown surface")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPut {
		var body navigation.Snapshot
		raw, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if readErr != nil || plugins.DecodeStrict(raw, &body) != nil {
			BadRequest(w, "invalid menu body")
			return
		}
		if err := h.services.Navigation.Save(r.Context(), c.UserID, surface, body); err != nil {
			switch {
			case errors.Is(err, navigation.ErrConflict):
				writeJSON(w, http.StatusConflict, err.Error(), nil)
			case errors.Is(err, navigation.ErrPermission):
				Forbidden(w, err.Error())
			case errors.Is(err, navigation.ErrInvalid):
				BadRequest(w, err.Error())
			default:
				ServerError(w, err)
			}
			return
		}
	}
	snapshot, err := h.services.Navigation.Read(r.Context(), surface)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, snapshot)
}
