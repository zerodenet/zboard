package handler

import (
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"net/http"
)

func (h *handlers) createQuotaRequest(w http.ResponseWriter, r *http.Request, claims authClaims, req taskCreateReq) {
	var content entitlements.QuotaAdjustment
	if err := json.Unmarshal(req.Content, &content); err != nil {
		BadRequest(w, "配额调整内容格式无效")
		return
	}
	out, err := h.services.QuotaRequests().Create(r.Context(), claims.UserID, entitlements.QuotaRequestInput{Scope: entitlements.QuotaScope{UserIDs: req.Scope.UserIDs, SubscriptionIDs: req.Scope.SubscriptionIDs, AllActive: req.Scope.AllActive}, Content: content, IdempotencyKey: req.IdempotencyKey, Priority: req.Priority, MaxAttempts: req.MaxAttempts, AutoRun: req.AutoRun})
	switch {
	case errors.Is(err, entitlements.ErrAccessPermission), errors.Is(err, entitlements.ErrAdministrativeRead):
		Forbidden(w, "current administrator required")
	case errors.Is(err, entitlements.ErrQuotaRequestInvalid):
		BadRequest(w, "配额调整量、原因或目标范围无效，请检查后重试")
	case errors.Is(err, entitlements.ErrQuotaRequestConflict):
		writeJSON(w, http.StatusConflict, err.Error(), nil)
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, "quota request failed", nil)
	default:
		if req.AutoRun {
			h.StartAdminTaskWorker()
		}
		OK(w, out)
	}
}
