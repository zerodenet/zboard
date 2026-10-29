package handler

import (
	"github.com/zerodenet/zboard/backend/internal/readtiming"
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
	finish := readtiming.Start(r.Context(), "dashboard_rankings")
	response, err := h.services.Dashboard.Rankings(r.Context(), period, dimension)
	finish()
	if err != nil {
		dashboardReadError(w, err)
		return
	}
	OK(w, response)
}
