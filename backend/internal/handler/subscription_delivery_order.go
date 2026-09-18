package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
)

func deliveryOrderKey(endpoint, entry uint) string { return network.DeliveryKey(endpoint, entry) }
func (h *handlers) SubscriptionDeliveryOrderHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	var out network.DeliverySnapshot
	if r.Method == http.MethodGet {
		out, err = h.services.DeliveryOrder.Read(r.Context(), actor.UserID)
	} else {
		var request network.DeliveryOrderRequest
		if err := decodeBody(r, &request); err != nil {
			BadRequest(w, err.Error())
			return
		}
		out, err = h.services.DeliveryOrder.Update(r.Context(), actor.UserID, request)
	}
	var invalid *network.DeliveryValidation
	switch {
	case errors.Is(err, network.ErrDeliveryPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	case errors.Is(err, network.ErrDeliveryVersionRequired):
		writeJSON(w, http.StatusPreconditionRequired, "请先加载当前订阅展示顺序。", nil)
	case errors.Is(err, network.ErrDeliveryConflict):
		writeJSON(w, http.StatusConflict, "服务列表或展示顺序已更新，请重新加载后保存。", nil)
	case errors.As(err, &invalid):
		BadRequestFields(w, "订阅展示顺序校验失败。", map[string]string{"ordered_keys": invalid.Message})
	case err != nil:
		ServerError(w, errors.New("subscription delivery order operation failed"))
	default:
		if r.Method == http.MethodGet {
			OK(w, out)
		} else {
			OK(w, struct {
				network.DeliverySnapshot
				Effect        string `json:"effect"`
				PublishStatus string `json:"publish_status"`
			}{out, "delivery", "not_required"})
		}
	}
}
