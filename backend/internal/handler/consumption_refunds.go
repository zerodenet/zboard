package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func consumptionRefundError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, commerce.ErrConsumptionRefundReview):
		writeJSON(w, http.StatusConflict, "有效订阅不支持保留权益的部分退款。请在订阅管理页整份退订并终止全部剩余权益；默认按剩余价值退款，管理员可覆盖为不超过可退实付的金额（含0元）；本次未退款或修改权益。", nil)
	case errors.Is(err, commerce.ErrConsumptionRefundConflict):
		writeJSON(w, http.StatusConflict, "该退款标识已用于不同内容，请先核对已有退款记录。", nil)
	case errors.Is(err, commerce.ErrSubscriptionSettlementConflict):
		writeJSON(w, http.StatusConflict, "退款超过原订单尚未退还或转换的实付金额，或来源记录不一致，请核对原订单结算。", nil)
	case errors.Is(err, commerce.ErrSubscriptionValueUnavailable):
		writeJSON(w, http.StatusConflict, "原订单缺少可核对的实际付款或结算依据，不能自动批准退款。", nil)
	case errors.Is(err, commerce.ErrNotFound):
		NotFound(w)
	default:
		walletError(w, err)
	}
}

func (h *handlers) AdminOrderConsumptionRefundsHandler(w http.ResponseWriter, r *http.Request) {
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
		BadRequest(w, "退款请求无效。")
		return
	}
	in, err := catalog.DecodeObject[commerce.ConsumptionRefundRequest](raw)
	if err != nil {
		BadRequest(w, "退款请求包含无效或多余字段。")
		return
	}
	out, err := h.services.ConsumptionRefunds.Approve(r.Context(), actor.UserID, id, in)
	if err != nil {
		consumptionRefundError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}

func (h *handlers) AdminConsumptionRefundHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/consumption-refunds/")
	if err != nil {
		BadRequest(w, "退款编号无效。")
		return
	}
	out, err := h.services.ConsumptionRefunds.Read(r.Context(), actor.UserID, id)
	if err != nil {
		consumptionRefundError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
