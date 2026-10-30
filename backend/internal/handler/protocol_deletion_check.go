package handler

import (
	"errors"
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
	"strings"
)

func (h *handlers) ProtocolEndpointDeletionCheckHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/deletion-check"), "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	check, err := h.services.ProtocolEndpointRemoval.Check(r.Context(), claims.UserID, id)
	if errors.Is(err, networkcap.ErrResourceNotFound) {
		NotFound(w)
		return
	}
	if errors.Is(err, networkcap.ErrResourcePermission) {
		Forbidden(w, "需要管理员权限。")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, check)
}

func (h *handlers) NetworkEntryDeletionCheckHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(strings.TrimSuffix(r.URL.Path, "/deletion-check"), "/api/v1/admin/network-entries/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	check, err := h.services.TopologyRemoval.Check(r.Context(), claims.UserID, id)
	if err != nil {
		resourceRemovalError(w, err)
		return
	}
	OK(w, check)
}
