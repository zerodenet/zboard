package handler

import (
	"net/http"
	"strconv"
	"strings"
)

func (h *handlers) AdminSubscriptionFairUseStateHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/state")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.FairUseObservations.State(r.Context(), actor.UserID, id)
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	OK(w, out)
}
func (h *handlers) AdminSubscriptionFairUseEventsHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/events")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			BadRequest(w, "limit must be between 1 and 200")
			return
		}
	}
	out, err := h.services.FairUseObservations.Events(r.Context(), actor.UserID, id, limit)
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	OK(w, out)
}
