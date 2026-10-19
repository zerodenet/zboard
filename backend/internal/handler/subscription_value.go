package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func subscriptionValueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	case errors.Is(err, commerce.ErrSubscriptionQuoteChanged):
		writeJSON(w, http.StatusConflict, "用量、权益或残值已变化，请重新核对报价后确认。", nil)
	case errors.Is(err, commerce.ErrSubscriptionSettlementConflict):
		writeJSON(w, http.StatusConflict, "退订记录或请求已变化，请先核对订阅和余额流水。", nil)
	case errors.Is(err, commerce.ErrSubscriptionValueCyclePending):
		writeJSON(w, http.StatusConflict, "流量周期正在重置，请稍后重新核对残值。", nil)
	case errors.Is(err, commerce.ErrSubscriptionValueUnavailable):
		writeJSON(w, http.StatusConflict, "成交或权益依据不完整，无法自动结算残值，请通过工单核对。", nil)
	default:
		walletError(w, err)
	}
}
func (h *handlers) SubscriptionValueHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/subscriptions/")
	if err != nil {
		BadRequest(w, "订阅编号无效。")
		return
	}
	if r.Method == http.MethodGet {
		out, err := h.services.SubscriptionValues.Read(r.Context(), claims.UserID, id)
		if err != nil {
			subscriptionValueError(w, err)
			return
		}
		OK(w, out)
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "退订请求无效。")
		return
	}
	in, err := catalog.DecodeObject[commerce.SubscriptionTerminationRequest](raw)
	if err != nil {
		BadRequest(w, "退订不接受金额、币种或其他多余字段。")
		return
	}
	out, err := h.services.SubscriptionTermination.Terminate(r.Context(), claims.UserID, id, in)
	if err != nil {
		subscriptionValueError(w, err)
		return
	}
	OK(w, out)
}
