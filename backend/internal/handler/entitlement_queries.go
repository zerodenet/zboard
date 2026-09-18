package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"net/http"
	"strconv"
	"strings"
)

func (h *handlers) SubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/subscriptions")
	var actor authClaims
	var err error
	if admin {
		actor, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	} else {
		actor, err = h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, err.Error())
			return
		}
	}
	q := entitlements.SubscriptionQuery{LegacyArray: !wantsPagedList(r), Status: r.URL.Query().Get("status"), EligibleFor: r.URL.Query().Get("eligible_for"), Search: r.URL.Query().Get("q")}
	q.ID, err = positiveQueryID(r.URL.Query(), "subscription_id")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if admin {
		if target := strings.TrimSpace(r.URL.Query().Get("user_id")); target != "" {
			id, e := strconv.ParseUint(target, 10, 64)
			if e != nil || id == 0 {
				BadRequest(w, "invalid user_id")
				return
			}
			q.UserID = uint(id)
		}
		q.Quota = r.URL.Query().Get("quota")
		window, present, e := parseOptionalDateWindow(r.URL.Query(), "expires_from", "expires_to", historyMaxWindowDays)
		if e != nil {
			BadRequest(w, e.Error())
			return
		}
		if present {
			q.From = window.From
			q.To = window.To
		}
	}
	if !q.LegacyArray {
		q.Offset, q.Limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	var out entitlements.SubscriptionPage
	if admin {
		out, err = h.services.SubscriptionQueries.Administrative(r.Context(), actor.UserID, q)
	} else {
		out, err = h.services.SubscriptionQueries.Owned(r.Context(), actor.UserID, q)
	}
	if err != nil {
		writeSubscriptionQueryError(w, err)
		return
	}
	if q.LegacyArray {
		OK(w, out.Legacy)
	} else {
		OK(w, pagedData(out.Items, out.Total, out.Offset, out.Limit))
	}
}
func (h *handlers) AdminSubscriptionGetHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscriptions/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.SubscriptionQueries.Detail(r.Context(), actor.UserID, id)
	if err != nil {
		writeSubscriptionQueryError(w, err)
		return
	}
	OK(w, out)
}
func writeSubscriptionQueryError(w http.ResponseWriter, err error) {
	var invalid *entitlements.QueryError
	switch {
	case errors.Is(err, entitlements.ErrAccessPermission) || errors.Is(err, entitlements.ErrAdministrativeRead):
		Forbidden(w, "当前账号无权读取这些订阅。")
	case errors.Is(err, entitlements.ErrAccessNotFound):
		NotFound(w)
	case errors.As(err, &invalid):
		BadRequest(w, invalid.Error())
	default:
		ServerError(w, errors.New("subscription query failed"))
	}
}
