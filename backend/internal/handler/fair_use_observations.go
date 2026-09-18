package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
	"time"
)

type fairUseObservationRange = metering.ObservationRange
type fairUseObservationBucket = metering.ObservationBucket
type fairUseObservationSeries = metering.ObservationSeries

func parseFairUseObservationRange(raw string) (fairUseObservationRange, error) {
	return metering.ParseObservationRange(raw)
}
func fairUsePercentile(values []int64, p float64) int64 {
	return metering.ObservationPercentile(values, p)
}
func (h *handlers) AdminSubscriptionFairUseObservationsHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/observations")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.FairUseObservationSeries.Read(r.Context(), actor.UserID, id, r.URL.Query().Get("range"), time.Now().UTC())
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	OK(w, out)
}
