package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

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
