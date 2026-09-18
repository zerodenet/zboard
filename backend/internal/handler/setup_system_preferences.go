package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net/http"
)

type setupInstallWithPreferencesRequest = platform.InstallationInput
type setupSystemPreferences = platform.SetupPreferences

func defaultSetupSystemPreferences() setupSystemPreferences {
	return platform.DefaultSetupPreferences()
}
func normalizeSetupSystemPreferences(body setupInstallWithPreferencesRequest) (setupSystemPreferences, error) {
	prefs, err := platform.NormalizeSetupPreferences(body)
	var invalid *platform.InstallationValidation
	if errors.As(err, &invalid) {
		return setupSystemPreferences{}, validationError(invalid.Error(), invalid.Fields)
	}
	return prefs, err
}
func (h *handlers) SetupInstallWithSystemPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	var body setupInstallWithPreferencesRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.installPlatform(w, r, body, true)
}
func (h *handlers) installPlatform(w http.ResponseWriter, r *http.Request, body platform.InstallationInput, includePreferences bool) {
	result, err := h.services.Installation.Create(r.Context(), body)
	var invalid *platform.InstallationValidation
	if errors.As(err, &invalid) {
		BadRequestError(w, validationError(invalid.Error(), invalid.Fields))
		return
	}
	if errors.Is(err, platform.ErrAlreadyInstalled) {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "installation failed", nil)
		return
	}
	token, expiresAt, err := h.issueToken(authClaims{UserID: result.Account.ID, Email: result.Account.Email, IsAdmin: result.Account.IsAdmin})
	if err != nil {
		ServerError(w, err)
		return
	}
	data := map[string]interface{}{"installed": true, "site_name": result.SiteName, "user": result.Account, "auth": tokenResponse{Token: token, ExpiresAt: expiresAt}}
	if includePreferences {
		data["system_preferences"] = result.Preferences
	}
	OK(w, data)
}
