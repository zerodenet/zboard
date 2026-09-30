package handler

import (
	"errors"
	"net/http"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type protocolEndpointUsageResetRequest struct {
	Reason string `json:"reason"`
}

func (h *handlers) ProtocolEndpointUsageResetsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	endpointID, err := parsePathID(r.URL.Path, "/api/v1/admin/protocol-endpoints/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if r.Method == http.MethodGet {
		rows, err := h.services.ProtocolEndpointUsagePeriods.History(r.Context(), claims.UserID, endpointID)
		if !writeProtocolEndpointUsagePeriodError(w, err) {
			OK(w, rows)
		}
		return
	}
	var req protocolEndpointUsageResetRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	reset, err := h.services.ProtocolEndpointUsagePeriods.Reset(r.Context(), claims.UserID, endpointID, req.Reason)
	if !writeProtocolEndpointUsagePeriodError(w, err) {
		OK(w, reset)
	}
}

func writeProtocolEndpointUsagePeriodError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, networkcap.ErrProtocolEndpointUsageNotFound):
		NotFound(w)
	case errors.Is(err, networkcap.ErrProtocolEndpointUsagePermission):
		Forbidden(w, "管理员权限已失效。")
	default:
		var validation *networkcap.ProtocolEndpointUsagePeriodValidation
		if errors.As(err, &validation) {
			BadRequest(w, validation.Error())
		} else {
			ServerError(w, err)
		}
	}
	return true
}
