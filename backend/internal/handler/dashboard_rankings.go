package handler

import (
	"net/http"
	"time"
)

func (h *handlers) DashboardTrafficRankingsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	period, err := resolveDashboardPeriodInLocation(r.URL.Query().Get("range"), time.Now().UTC(), h.systemTimezoneLocation())
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	dimension := r.URL.Query().Get("dimension")
	if dimension != "" && dimension != "nodes" && dimension != "users" {
		BadRequest(w, "dimension must be nodes or users")
		return
	}
	response, err := h.services.Dashboard.Rankings(r.Context(), period, dimension)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, response)
}
