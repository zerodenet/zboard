package handler

import "net/http"

func (h *handlers) CommerceDiscountProvidersHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	providers, err := h.services.DiscountProviders.List(r.Context(), claims.UserID)
	if err != nil {
		writeCapabilityError(w, http.StatusServiceUnavailable, "优惠提供方暂不可用，您仍可选择不使用优惠。", "discount_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, providers)
}
