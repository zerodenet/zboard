package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func (h *handlers) AdminPluginAuthorizationHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var body struct {
		Generation    uint64   `json:"generation"`
		Digest        string   `json:"digest"`
		Capabilities  []string `json:"capabilities"`
		NativeTrusted bool     `json:"native_trusted"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	v, err := h.pluginManager.Authorize(pathvar.Vars(r)["id"], claims.Email, body.Digest, body.Generation, body.Capabilities, body.NativeTrusted)
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, v)
}
func (h *handlers) AdminPluginMigrationsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	rows, err := h.pluginManager.Migrations(pathvar.Vars(r)["id"])
	if err != nil {
		pluginError(w, err)
		return
	}
	OK(w, rows)
}
