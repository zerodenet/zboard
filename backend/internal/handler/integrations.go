package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"io"
	"net/http"
	"strconv"
	"strings"

	capabilityapi "github.com/zerodenet/zboard/backend/pkg/capabilityapi/v1"
)

func writeCapabilityError(w http.ResponseWriter, status int, message string, code capabilityapi.ErrorCode) {
	retryable := capabilityapi.IsRetryable(code)
	writeJSONResponse(w, status, message, nil, &APIError{Version: 1, Code: string(code), Retryable: &retryable})
}

func integrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrPermission):
		writeCapabilityError(w, http.StatusForbidden, "integration credential access denied", capabilityapi.ErrorPermissionDenied)
	case errors.Is(err, catalog.ErrDenied):
		writeCapabilityError(w, http.StatusUnauthorized, "invalid or unauthorized integration credential", capabilityapi.ErrorUnauthenticated)
	case errors.Is(err, identity.ErrIntegrationInput), errors.Is(err, catalog.ErrInput):
		writeCapabilityError(w, http.StatusBadRequest, "invalid integration request", capabilityapi.ErrorInvalidArgument)
	case errors.Is(err, catalog.ErrUnknown):
		writeCapabilityError(w, http.StatusNotFound, "unknown capability", capabilityapi.ErrorNotFound)
	case errors.Is(err, catalog.ErrConflict):
		writeCapabilityError(w, http.StatusConflict, "capability state conflict", capabilityapi.ErrorConflict)
	case errors.Is(err, catalog.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeCapabilityError(w, http.StatusTooManyRequests, "capability rate limit exceeded", capabilityapi.ErrorRateLimited)
	case errors.Is(err, catalog.ErrUnavailable):
		writeCapabilityError(w, http.StatusServiceUnavailable, "integration service unavailable", capabilityapi.ErrorUnavailable)
	case errors.Is(err, context.DeadlineExceeded):
		writeCapabilityError(w, http.StatusGatewayTimeout, "capability deadline exceeded", capabilityapi.ErrorDeadlineExceeded)
	case errors.Is(err, context.Canceled):
		writeCapabilityError(w, http.StatusRequestTimeout, "request canceled", capabilityapi.ErrorDeadlineExceeded)
	default:
		writeCapabilityError(w, http.StatusInternalServerError, "integration service unavailable", capabilityapi.ErrorUnavailable)
	}
}
func integrationBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, error) {
	if r.Body == nil {
		return nil, catalog.ErrInput
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, catalog.MaxInputBytes))
	if err != nil {
		return nil, catalog.ErrInput
	}
	return raw, nil
}
func (h *handlers) IntegrationCredentialCreateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		integrationError(w, err)
		return
	}
	in, err := catalog.DecodeObject[identity.IntegrationIssue](raw)
	if err != nil {
		integrationError(w, err)
		return
	}
	result, err := h.services.Integrations().Issue(r.Context(), claims.UserID, in)
	if err != nil {
		integrationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, result)
}
func (h *handlers) IntegrationCredentialListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	rows, err := h.services.Integrations().List(r.Context(), claims.UserID, offset, limit)
	if err != nil {
		integrationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, map[string]interface{}{"items": rows, "offset": offset, "limit": limit})
}
func (h *handlers) IntegrationCredentialRevokeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	id, err := strconv.ParseUint(pathvar.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		BadRequest(w, "invalid credential id")
		return
	}
	if err := h.services.Integrations().Revoke(r.Context(), claims.UserID, uint(id)); err != nil {
		integrationError(w, err)
		return
	}
	OK(w, map[string]bool{"revoked": true})
}
func integrationCredential(r *http.Request) (catalog.Credential, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "zbi_") {
		return catalog.Credential{}, catalog.ErrDenied
	}
	return catalog.Credential{Kind: "integration", Proof: parts[1]}, nil
}
func (h *handlers) integrationRegistry() (*catalog.Registry, error) {
	registry := catalog.New(h.services.IntegrationAuthority(), h.services.IntegrationAdmission())
	if err := h.services.RegisterMeteringCapabilities(registry, trafficStatisticsCacheAdapter{&h.trafficStatisticsCache}, h.trafficIncrementalStats); err != nil {
		return nil, err
	}
	if err := h.services.RegisterCommerceCapabilities(registry, h.credentialCipher, h.zeroMieruAccess); err != nil {
		return nil, err
	}
	return registry, nil
}
func (h *handlers) IntegrationCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	credential, err := integrationCredential(r)
	if err != nil {
		integrationError(w, err)
		return
	}
	if err := h.services.AuthenticateIntegration(r.Context(), credential); err != nil {
		integrationError(w, err)
		return
	}
	registry, err := h.integrationRegistry()
	if err != nil {
		integrationError(w, err)
		return
	}
	descriptors, err := registry.List(r.Context(), credential)
	if err != nil {
		integrationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, map[string]interface{}{"items": descriptors})
}
func (h *handlers) IntegrationInvokeHandler(w http.ResponseWriter, r *http.Request) {
	credential, err := integrationCredential(r)
	if err != nil {
		integrationError(w, err)
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		integrationError(w, err)
		return
	}
	registry, err := h.integrationRegistry()
	if err != nil {
		integrationError(w, err)
		return
	}
	result, err := registry.Invoke(r.Context(), credential, pathvar.Vars(r)["name"], raw)
	if err != nil {
		integrationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, result)
}
