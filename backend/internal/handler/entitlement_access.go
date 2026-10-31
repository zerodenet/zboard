package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net/http"
)

func (h *handlers) AccountSubscriptionAccessHandler(w http.ResponseWriter, r *http.Request) {
	h.accountAccess(w, r, "read")
}
func (h *handlers) AccountSubscriptionAccessRotateHandler(w http.ResponseWriter, r *http.Request) {
	h.accountAccess(w, r, "rotate")
}
func (h *handlers) AccountSubscriptionAccessRevokeHandler(w http.ResponseWriter, r *http.Request) {
	h.accountAccess(w, r, "revoke")
}
func (h *handlers) accountAccess(w http.ResponseWriter, r *http.Request, operation string) {
	w.Header().Set("Cache-Control", "no-store")
	actor, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	id, err := parseAccountSubscriptionAccessID(r.URL.Path, operation == "rotate")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	service := h.services.SubscriptionAccess(h.credentialCipher)
	var out entitlements.AccessView
	switch operation {
	case "read":
		out, err = service.Read(r.Context(), actor.UserID, id)
	case "rotate":
		out, err = service.Rotate(r.Context(), actor.UserID, id)
	case "revoke":
		out, err = service.Revoke(r.Context(), actor.UserID, id)
	}
	switch {
	case errors.Is(err, entitlements.ErrAccessNotFound):
		NotFound(w)
	case errors.Is(err, entitlements.ErrAccessPermission):
		Forbidden(w, "当前账号无权管理订阅访问。")
	case errors.Is(err, entitlements.ErrAccessInactive):
		Forbidden(w, err.Error())
	case err != nil:
		ServerError(w, errors.New("subscription access operation failed"))
	default:
		if err := h.completeAccessURL(r.Context(), &out); err != nil {
			ServerError(w, errors.New("subscription delivery setting unavailable"))
			return
		}
		OK(w, out)
	}
}
func (h *handlers) completeAccessURL(ctx context.Context, view *entitlements.AccessView) error {
	row, err := h.services.Settings.Get(ctx, platform.SubscriptionPrefixKey)
	if err != nil && !errors.Is(err, platform.ErrSettingNotFound) {
		return err
	}
	view.DeliveryConfigured = row.Value != ""
	if !view.DeliveryConfigured {
		view.Configured = false
		view.Token = ""
		view.Notice = "管理员尚未配置订阅地址前缀，请联系管理员。"
		return nil
	}
	if view.Configured && view.Token != "" {
		view.SubscriptionURL, err = platform.SubscriptionDeliveryURL(row.Value, view.Token)
		if err != nil {
			return err
		}
	}
	return nil
}
