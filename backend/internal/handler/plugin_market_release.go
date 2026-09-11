package handler

import (
	"github.com/zeromicro/go-zero/rest/pathvar"
	"net/http"
)

func (h *handlers) AdminPluginMarketDetailHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	detail, err := h.pluginManager.MarketDetail(r.Context(), pathvar.Vars(r)["id"], r.URL.Query().Get("version"))
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, detail)
}
func (h *handlers) AdminPluginMarketInspectHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	preview, err := h.pluginManager.PreviewMarket(r.Context(), pathvar.Vars(r)["id"], r.URL.Query().Get("version"))
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, preview)
}
func (h *handlers) AdminPluginMarketInstallHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body struct {
		Version     string `json:"version"`
		Digest      string `json:"digest"`
		Fingerprint string `json:"fingerprint"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	item, err := h.pluginManager.InstallMarketConfirmed(r.Context(), pathvar.Vars(r)["id"], body.Version, body.Digest, body.Fingerprint, claims.Email)
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, item)
}
