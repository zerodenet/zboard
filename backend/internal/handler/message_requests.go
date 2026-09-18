package handler

import (
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"net/http"
)

func (h *handlers) createMailRequest(w http.ResponseWriter, r *http.Request, claims authClaims, req taskCreateReq) {
	var content messaging.EmailContent
	if err := json.Unmarshal(req.Content, &content); err != nil {
		BadRequest(w, "邮件内容格式无效")
		return
	}
	out, err := h.services.MessageRequests().Create(r.Context(), claims.UserID, messaging.RequestInput{Scope: messaging.RecipientScope{UserIDs: req.Scope.UserIDs, AllActive: req.Scope.AllActive}, Content: content, IdempotencyKey: req.IdempotencyKey, Priority: req.Priority, MaxAttempts: req.MaxAttempts, AutoRun: req.AutoRun})
	switch {
	case errors.Is(err, messaging.ErrTemplatePermission):
		Forbidden(w, "current administrator required")
	case errors.Is(err, messaging.ErrInvalidMessage):
		BadRequest(w, "邮件内容、模板版本或收件范围无效，请刷新后重试")
	case errors.Is(err, messaging.ErrRequestConflict):
		writeJSON(w, http.StatusConflict, err.Error(), nil)
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, "message request failed", nil)
	default:
		if req.AutoRun {
			h.StartAdminTaskWorker()
		}
		OK(w, out)
	}
}
