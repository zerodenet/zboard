package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (h *handlers) AdminSubscriptionQuotaHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/quota"), "/api/v1/admin/subscriptions/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var req entitlements.SubscriptionQuotaInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		BadRequest(w, "请填写需要修改的流量参数和调整原因。")
		return
	}
	req.SubscriptionID = id
	err = h.services.SubscriptionQuota(h.credentialCipher, h.zeroMieruAccess).Update(r.Context(), actor.UserID, req)
	switch {
	case errors.Is(err, entitlements.ErrSubscriptionQuotaInvalid):
		BadRequest(w, "流量参数或调整原因无效。")
	case errors.Is(err, entitlements.ErrSubscriptionQuotaConflict):
		writeJSON(w, http.StatusConflict, "请求标识已用于不同的修改，请重新提交。", nil)
	case err != nil:
		writeSubscriptionQueryError(w, err)
	default:
		OK(w, map[string]any{"subscription_id": id})
	}
}
