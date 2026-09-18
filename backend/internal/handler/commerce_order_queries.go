package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
	"strconv"
	"strings"
)

func (h *handlers) OrderListHandler(w http.ResponseWriter, r *http.Request) {
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/orders")
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
	q := commerce.OrderQuery{Status: r.URL.Query().Get("status"), LegacyArray: !wantsPagedList(r)}
	if admin {
		if value := strings.TrimSpace(r.URL.Query().Get("user_id")); value != "" {
			id, e := strconv.ParseUint(value, 10, 64)
			if e != nil || id == 0 {
				BadRequest(w, "invalid user_id")
				return
			}
			q.UserID = uint(id)
		}
		q.Search = r.URL.Query().Get("q")
		q.OrderType = r.URL.Query().Get("order_type")
		window, present, e := parseOptionalDateWindow(r.URL.Query(), "created_from", "created_to", historyMaxWindowDays)
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
	var out commerce.OrderPage
	if admin {
		out, err = h.services.OrderQueries.Administrative(r.Context(), actor.UserID, q)
	} else {
		out, err = h.services.OrderQueries.Owned(r.Context(), actor.UserID, q)
	}
	if err != nil {
		writeOrderQueryError(w, err)
		return
	}
	if q.LegacyArray {
		OK(w, out.Items)
	} else {
		OK(w, pagedData(out.Items, out.Total, out.Offset, out.Limit))
	}
}
func (h *handlers) AdminOrderGetHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.OrderQueries.Detail(r.Context(), actor.UserID, id)
	if err != nil {
		writeOrderQueryError(w, err)
		return
	}
	OK(w, out)
}
func (h *handlers) AdminOrderPaymentEventsHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.OrderQueries.Events(r.Context(), actor.UserID, id, offset, limit)
	if err != nil {
		writeOrderQueryError(w, err)
		return
	}
	OK(w, pagedData(out.Items, out.Total, out.Offset, out.Limit))
}
func writeOrderQueryError(w http.ResponseWriter, err error) {
	var invalid *commerce.ValidationError
	switch {
	case errors.Is(err, commerce.ErrOrderPermission):
		Forbidden(w, "当前账号无权读取这些订单。")
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	case errors.As(err, &invalid):
		BadRequestError(w, commerceValidationError(err))
	default:
		writeCommercePersistenceFailure(w, "订单读取失败，请稍后重试。")
	}
}
