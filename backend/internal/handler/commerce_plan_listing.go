package handler

import (
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) PlanListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	q := commerce.PlanListQuery{LegacyArray: r.URL.Query().Get("paged") != "true", Operation: r.URL.Query().Get("operation"), Search: r.URL.Query().Get("q"), IncludeInactive: parseBoolQuery(r.URL.Query().Get("include_inactive"))}
	var err error
	if q.PlanID, err = positiveQueryID(r.URL.Query(), "plan_id"); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if q.ExcludePlanID, err = positiveQueryID(r.URL.Query(), "exclude_plan_id"); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if !q.LegacyArray {
		if q.Offset, q.Limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit")); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	claims, claimErr := h.authFromRequest(r)
	admin := claimErr == nil && claims.IsAdmin
	if admin {
		if raw := strings.TrimSpace(r.URL.Query().Get("active")); raw != "" {
			active, err := parseStrictBool(raw)
			if err != nil {
				BadRequest(w, "invalid active")
				return
			}
			q.Active = &active
		}
	}
	var result commerce.PlanListResult
	if admin {
		result, err = h.services.PlanListing.Administrative(r.Context(), claims.UserID, q)
	} else {
		result, err = h.services.PlanListing.Public(r.Context(), q)
	}
	if err != nil {
		skuQueryError(w, err)
		return
	}
	if q.LegacyArray {
		OK(w, result.Legacy)
	} else {
		OK(w, pagedData(result.Items, result.Total, result.Offset, result.Limit))
	}
}
