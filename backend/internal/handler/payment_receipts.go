package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
	"strconv"
	"strings"
)

func (h *handlers) PaymentReceiptsHandler(w http.ResponseWriter, r *http.Request) {
	admin := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	prefix := "/api/v1/orders/"
	if admin {
		prefix = "/api/v1/admin/orders/"
		claims, err = h.requireAdmin(w, r)
		if err != nil {
			return
		}
	}
	id, err := parsePathID(r.URL.Path, prefix)
	if err != nil {
		BadRequest(w, "订单编号无效。")
		return
	}
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			BadRequest(w, "收据游标无效。")
			return
		}
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			BadRequest(w, "收据分页无效。")
			return
		}
	}
	out, err := h.services.PaymentReceipts.Read(r.Context(), claims.UserID, id, admin, uint(after), limit)
	if err != nil {
		writeOrderQueryError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
func (h *handlers) ResolvePaymentReceiptHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/orders/")
	if err != nil {
		BadRequest(w, "订单编号无效。")
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "收据核对请求无效。")
		return
	}
	in, err := catalog.DecodeObject[commerce.ResolveReceiptRequest](raw)
	if err != nil {
		BadRequest(w, "核对请求包含无效或多余字段。")
		return
	}
	out, err := h.services.PaymentReceipts.Resolve(r.Context(), actor.UserID, id, in)
	if err != nil {
		if errors.Is(err, commerce.ErrPaymentFactConflict) {
			writeJSON(w, http.StatusConflict, "收据已处理或本次核对结论与原处理不一致。", nil)
			return
		}
		writeOrderQueryError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
