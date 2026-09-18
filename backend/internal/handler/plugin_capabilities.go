package handler

import (
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	capabilityapi "github.com/zerodenet/zboard/backend/pkg/capabilityapi/v1"
	"net/http"
)

func (h *handlers) pluginCapabilityCall(w http.ResponseWriter, r *http.Request, claims authClaims, kind, name string, input json.RawMessage) {
	authority := plugins.SessionAuthority{Manager: h.pluginManager, UserID: claims.UserID, Admin: claims.IsAdmin}
	registry := catalog.New(authority, authority)
	if err := h.services.RegisterMeteringCapabilities(registry, trafficStatisticsCacheAdapter{&h.trafficStatisticsCache}, h.trafficIncrementalStats); err != nil {
		pluginCapabilityError(w, err)
		return
	}
	if err := h.services.RegisterCommerceCapabilities(registry, h.credentialCipher, h.zeroMieruAccess); err != nil {
		pluginCapabilityError(w, err)
		return
	}
	credential := catalog.Credential{Kind: "plugin_session", Proof: r.Header.Get("X-Plugin-Session")}
	if kind == "capabilities.list" {
		items, err := registry.List(r.Context(), credential)
		if err != nil {
			pluginCapabilityError(w, err)
			return
		}
		OK(w, map[string]interface{}{"items": items})
		return
	}
	if kind != "capabilities.invoke" {
		Forbidden(w, "unsupported plugin capability")
		return
	}
	result, err := registry.Invoke(r.Context(), credential, name, input)
	if err != nil {
		pluginCapabilityError(w, err)
		return
	}
	OK(w, result)
}

func pluginCapabilityError(w http.ResponseWriter, err error) {
	if errors.Is(err, catalog.ErrDenied) {
		writeCapabilityError(w, http.StatusForbidden, "plugin capability not granted", capabilityapi.ErrorPermissionDenied)
		return
	}
	if errors.Is(err, plugins.ErrUnavailable) {
		writeCapabilityError(w, http.StatusServiceUnavailable, "plugin temporarily unavailable", capabilityapi.ErrorUnavailable)
		return
	}
	integrationError(w, err)
}
