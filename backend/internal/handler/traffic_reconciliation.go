package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
	"strconv"
	"strings"
)

func (h *handlers) TrafficReconciliationHandler(w http.ResponseWriter, r *http.Request) {
	adminScope := strings.HasPrefix(r.URL.Path, "/api/v1/admin/traffic/")
	var claims authClaims
	var err error
	if adminScope {
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		claims, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}

	userID := claims.UserID
	if adminScope {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			parsed, parseErr := strconv.ParseUint(target, 10, 64)
			if parseErr != nil || parsed == 0 {
				BadRequest(w, "invalid user_id")
				return
			}
			userID = uint(parsed)
		} else {
			userID = 0
		}
	}

	var subscriptionID uint
	if target := strings.TrimSpace(r.URL.Query().Get("subscription_id")); target != "" {
		parsed, parseErr := strconv.ParseUint(target, 10, 64)
		if parseErr != nil || parsed == 0 {
			BadRequest(w, "invalid subscription_id")
			return
		}
		subscriptionID = uint(parsed)
	}
	issuesOnly := false
	if adminScope {
		if rawIssuesOnly := strings.TrimSpace(r.URL.Query().Get("issues_only")); rawIssuesOnly != "" {
			issuesOnly, err = strconv.ParseBool(rawIssuesOnly)
			if err != nil {
				BadRequest(w, "invalid issues_only")
				return
			}
		}
	}
	paged := adminScope && r.URL.Query().Get("paged") == "true"
	offset, limit := 0, 50
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	result, err := h.services.Reconciliation().Read(r.Context(), claims.UserID, metering.ReconciliationQuery{Administrative: adminScope, UserID: userID, SubscriptionID: subscriptionID, Paged: paged, IssuesOnly: issuesOnly, Offset: offset, Limit: limit})
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	if paged {
		data := pagedData(result.Items, result.Total, offset, limit)
		data["aggregates"] = result.Aggregates
		OK(w, data)
		return
	}
	OK(w, result.Items)
}
func trafficReconciliationResult(difference int64) string {
	return metering.ReconciliationResult(difference)
}
