package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func (h *handlers) AdminSubscriptionCredentialMigrationHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/credential-migration"), "/api/v1/admin/subscriptions/")
	if err != nil {
		BadRequest(w, "订阅编号无效。")
		return
	}
	service := h.services.CredentialMigration(h.credentialCipher, h.zeroMieruAccess)
	switch r.Method {
	case http.MethodGet:
		status, e := service.Status(r.Context(), actor.UserID, id)
		if e != nil {
			writeSubscriptionQueryError(w, e)
			return
		}
		OK(w, status)
		return
	case http.MethodPost:
		err = service.Prepare(r.Context(), actor.UserID, id)
	case http.MethodDelete:
		err = service.Rollback(r.Context(), actor.UserID, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if errors.Is(err, entitlements.ErrCredentialMigrationBlocked) {
		message := strings.TrimPrefix(err.Error(), entitlements.ErrCredentialMigrationBlocked.Error()+": ")
		writeJSON(w, http.StatusConflict, message, nil)
		return
	}
	if errors.Is(err, entitlements.ErrSubscriptionMutationInvalid) {
		BadRequest(w, "仅有效且仍有可用流量的订阅可以开始凭证迁移。")
		return
	}
	if err != nil {
		writeSubscriptionQueryError(w, err)
		return
	}
	status, err := service.Status(r.Context(), actor.UserID, id)
	if err != nil {
		writeSubscriptionQueryError(w, err)
		return
	}
	OK(w, status)
}
