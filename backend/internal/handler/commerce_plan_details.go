package handler

import "net/http"

func (h *handlers) PublicPlanDetailCommerceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.PlanDetails.Public(r.Context(), id)
	if err != nil {
		skuQueryError(w, err)
		return
	}
	OK(w, view)
}
func (h *handlers) PlanDetailHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	view, err := h.services.PlanDetails.Administrative(r.Context(), claims.UserID, id)
	if err != nil {
		skuQueryError(w, err)
		return
	}
	OK(w, view)
}
