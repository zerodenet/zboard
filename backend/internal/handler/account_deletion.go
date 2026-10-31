package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"net/http"
	"strings"
)

func (h *handlers) AdminUserDeletionPreviewHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/deletion-preview"), "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	preview, err := h.services.AccountDeletion().Preview(r.Context(), actor.UserID, id)
	if err != nil {
		writeAccountAdministrationError(w, err)
		return
	}
	OK(w, preview)
}
func (h *handlers) AdminUserDeleteHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	err = h.services.AccountDeletion().Delete(r.Context(), actor.UserID, id)
	var blocked *identity.DeletionBlocked
	if errors.As(err, &blocked) {
		// Keep the actionable reason in the deleting dialog, including a
		// condition which changed since its earlier preview.
		BadRequest(w, strings.Join(blocked.Preview.Blockers, " "))
		return
	}
	if err != nil {
		writeAccountAdministrationError(w, err)
		return
	}
	OK(w, map[string]any{"id": id, "deleted": true, "history_retained": true})
}
func (h *handlers) StartUserCleanupWorker() { h.startPeriodicJob(h.services.UserCleanupJob()) }
