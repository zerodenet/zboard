package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"net/http"
)

func writeFairUsePolicyError(w http.ResponseWriter, err error) {
	var invalid *metering.PolicyValidation
	switch {
	case errors.As(err, &invalid):
		BadRequestFields(w, invalid.Error(), invalid.Fields)
	case errors.Is(err, metering.ErrPolicyRevisionConflict):
		writeJSON(w, http.StatusConflict, "Fair Use policy revision conflict", nil)
	case errors.Is(err, metering.ErrPolicyNotFound):
		NotFound(w)
	case errors.Is(err, metering.ErrPolicyPermission):
		Forbidden(w, "需要当前有效的管理员权限。")
	default:
		ServerError(w, errors.New("Fair Use policy operation failed"))
	}
}
func (h *handlers) AdminPlatformFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	h.handleScopedFairUsePolicy(w, r, "platform", 0)
}
func (h *handlers) AdminPlanFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parseFairUseResourcePlanID(r.URL.Path, "/fair-use/policy")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.handleScopedFairUsePolicy(w, r, "plan", id)
}
func (h *handlers) AdminSubscriptionFairUsePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/policy")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.handleScopedFairUsePolicy(w, r, "subscription", id)
}
func (h *handlers) handleScopedFairUsePolicy(w http.ResponseWriter, r *http.Request, scopeType string, id uint) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	scope := metering.PolicyScope{Type: scopeType, ID: id}
	var out metering.PolicyResolution
	switch r.Method {
	case http.MethodGet:
		out, err = h.services.FairUsePolicies.Read(r.Context(), actor.UserID, scope)
	case http.MethodPut:
		input, decodeErr := decodeFairUsePolicyInput(r)
		if decodeErr != nil {
			BadRequest(w, decodeErr.Error())
			return
		}
		out, err = h.services.FairUsePolicies.Save(r.Context(), actor.UserID, scope, input)
	case http.MethodDelete:
		if scopeType == "platform" {
			BadRequest(w, "unsupported Fair Use policy method")
			return
		}
		out, err = h.services.FairUsePolicies.Delete(r.Context(), actor.UserID, scope)
	default:
		BadRequest(w, "unsupported Fair Use policy method")
		return
	}
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	OK(w, out)
}
