package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func fundingError(w http.ResponseWriter, err error) {
	if errors.Is(err, commerce.ErrFundingConflict) {
		writeJSON(w, http.StatusConflict, "订单资金分配已冻结或失效，请刷新订单核对。", nil)
		return
	}
	if errors.Is(err, commerce.ErrNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, commerce.ErrSubscriptionCapacity) {
		writePlanSubscriptionLimitReached(w)
		return
	}
	walletError(w, err)
}
func (h *handlers) OrderFundingHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/orders/")
	if err != nil {
		BadRequest(w, "订单编号无效。")
		return
	}
	service := h.services.WalletPayments(h.credentialCipher, h.zeroMieruAccess)
	var out commerce.OrderFundingView
	if r.Method == http.MethodGet {
		out, err = service.Read(r.Context(), actor.UserID, id)
	} else {
		raw, e := integrationBody(w, r)
		if e != nil {
			BadRequest(w, "资金分配请求无效。")
			return
		}
		request, e := catalog.DecodeObject[commerce.FundingRequest](raw)
		if e != nil {
			BadRequest(w, "资金分配包含无效或多余字段。")
			return
		}
		out, err = service.Configure(r.Context(), actor.UserID, id, request)
	}
	if err != nil {
		fundingError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
func (h *handlers) WalletPayOrderHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/orders/")
	if err != nil {
		BadRequest(w, "订单编号无效。")
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "付款请求无效。")
		return
	}
	if _, err := catalog.DecodeObject[struct{}](raw); err != nil {
		BadRequest(w, "余额付款不接受价格、金额或付款状态参数。")
		return
	}
	out, err := h.services.WalletPayments(h.credentialCipher, h.zeroMieruAccess).Pay(r.Context(), actor.UserID, id)
	if err != nil {
		fundingError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
