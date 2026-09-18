package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func (h *handlers) orderPayHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.OrderSettlement(h.credentialCipher, h.zeroMieruAccess).Apply(r.Context(), actor.UserID, commerce.SettlementCommand{OrderID: id, Status: "paid", Force: parseBoolQuery(r.URL.Query().Get("force"))})
	h.writeOrderSettlement(w, out, err, false)
}
func (h *handlers) orderPayCallbackHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseOrderID(r.URL.Path)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request orderCallbackReq
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	out, err := h.services.OrderSettlement(h.credentialCipher, h.zeroMieruAccess).Apply(r.Context(), actor.UserID, commerce.SettlementCommand{OrderID: id, Status: request.Status, Callback: &request.RawCallback})
	h.writeOrderSettlement(w, out, err, true)
}
func (h *handlers) writeOrderSettlement(w http.ResponseWriter, out commerce.SettlementResult, err error, callback bool) {
	var invalid *commerce.ValidationError
	switch {
	case errors.Is(err, commerce.ErrNotFound):
		BadRequest(w, "order not found")
	case errors.Is(err, commerce.ErrOrderPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	case errors.Is(err, commerce.ErrOrderTransition):
		if callback {
			BadRequest(w, errOrderTransitionRejected.Error())
		} else {
			BadRequest(w, errOrderNotPayable.Error())
		}
	case errors.Is(err, commerce.ErrSubscriptionCapacity):
		writePlanSubscriptionLimitReached(w)
	case errors.As(err, &invalid):
		BadRequestError(w, commerceValidationError(err))
	case err != nil:
		writeCommercePersistenceFailure(w, "订单结算失败，请稍后重试。")
	default:
		if out.Fulfilled {
		}
		OK(w, out.Order)
	}
}
