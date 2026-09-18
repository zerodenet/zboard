package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

type adminUserDetail = identity.AccountBusinessDetail

type adminSubscriptionDetail struct {
	adminSubscriptionListItem
	ActiveCredentialCount int64 `json:"active_credential_count"`
	TotalCredentialCount  int64 `json:"total_credential_count"`
}

func (h *handlers) AdminUserGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	detail, err := h.services.Identity.Directory.Detail(r.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrAccountNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, detail)
}
