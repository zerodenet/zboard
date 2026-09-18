package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
	"strings"
)

func (h *handlers) OrderCancelHandler(w http.ResponseWriter, r *http.Request) {
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/orders/")
	var claims authClaims
	var err error
	if admin {
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
	id, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	force := parseBoolQuery(r.URL.Query().Get("force"))
	if force && !admin {
		Forbidden(w, "force cancellation requires admin")
		return
	}
	var order commerce.Order
	if admin {
		order, err = h.services.OrderCancellation.Administrative(r.Context(), claims.UserID, id, force)
	} else {
		order, err = h.services.OrderCancellation.Owned(r.Context(), claims.UserID, id)
	}
	switch {
	case errors.Is(err, commerce.ErrOrderPermission):
		Forbidden(w, "no permission")
	case errors.Is(err, commerce.ErrNotFound):
		BadRequest(w, "order not found")
	case errors.Is(err, commerce.ErrOrderNotCancelable):
		BadRequest(w, err.Error())
	case err != nil:
		writeCommercePersistenceFailure(w, "订单取消失败，请稍后重试。")
	default:
		OK(w, order)
	}
}
