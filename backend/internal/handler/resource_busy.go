package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	capabilityapi "github.com/zerodenet/zboard/backend/pkg/capabilityapi/v1"
)

// Keep transient topology contention distinct from an invalid payment fact or
// an applied result. The browser must still explicitly retry its original intent.
func writeResourceBusy(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, network.ErrResourcesBusy) {
		return false
	}
	w.Header().Set("Retry-After", "1")
	writeCapabilityError(w, http.StatusServiceUnavailable, "接入资源正在变更，请保持原请求标识，核对当前状态后重试。", capabilityapi.ErrorUnavailable)
	return true
}
