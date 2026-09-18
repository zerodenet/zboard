package handler

import (
	"net/http"
)

func (h *handlers) ManagedCertificateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.ResourceRemoval.Certificate(r.Context(), claims.UserID, id)
	if err != nil {
		resourceRemovalError(w, err)
		return
	}
	OK(w, result)
}
