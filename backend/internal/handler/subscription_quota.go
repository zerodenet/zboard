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
	// Required pointers distinguish an intentional zero from omitted fields.
	var req struct {
		FlowTotal               *int64 `json:"flow_total"`
		FlowUsed                *int64 `json:"flow_used"`
		ResetQuotaBytes         *int64 `json:"reset_quota_bytes"`
		ExpectedFlowTotal       *int64 `json:"expected_flow_total"`
		ExpectedFlowUsed        *int64 `json:"expected_flow_used"`
		ExpectedResetQuotaBytes *int64 `json:"expected_reset_quota_bytes"`
		Reason                  string `json:"reason"`
		IdempotencyKey          string `json:"idempotency_key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.FlowTotal == nil || req.FlowUsed == nil || req.ResetQuotaBytes == nil || req.ExpectedFlowTotal == nil || req.ExpectedFlowUsed == nil || req.ExpectedResetQuotaBytes == nil {
		BadRequest(w, "请填写配额、已用流量、重置配额及原始值。")
		return
	}
	err = h.services.SubscriptionQuota(h.credentialCipher, h.zeroMieruAccess).Update(r.Context(), actor.UserID, entitlements.SubscriptionQuotaInput{SubscriptionID: id, FlowTotal: *req.FlowTotal, FlowUsed: *req.FlowUsed, ResetQuotaBytes: *req.ResetQuotaBytes, ExpectedFlowTotal: *req.ExpectedFlowTotal, ExpectedFlowUsed: *req.ExpectedFlowUsed, ExpectedResetQuotaBytes: *req.ExpectedResetQuotaBytes, Reason: req.Reason, IdempotencyKey: req.IdempotencyKey})
	switch {
	case errors.Is(err, entitlements.ErrSubscriptionQuotaInvalid):
		BadRequest(w, "流量参数或调整原因无效。")
	case errors.Is(err, entitlements.ErrSubscriptionQuotaConflict):
		writeJSON(w, http.StatusConflict, "订阅流量已发生变化，请刷新详情后重新调整。", nil)
	case err != nil:
		writeSubscriptionQueryError(w, err)
	default:
		OK(w, map[string]any{"subscription_id": id})
	}
}
