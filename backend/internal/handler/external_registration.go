package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"net/http"
)

type externalRegistrationInput struct {
	Email            string `json:"email"`
	VerificationCode string `json:"verification_code"`
}

var errExternalRegistrationEmail = identity.ErrVerifiedEmail

func (h *handlers) ExternalRegistrationCodeHandler(w http.ResponseWriter, r *http.Request) {
	origin, err := h.externalAuthOrigin(r.Context())
	if err != nil || !externalAuthRequest(r, origin) {
		Forbidden(w, "invalid authentication request")
		return
	}
	result, err := h.externalAuth.peek(cookieValue(r, origin, resultCookie))
	if err != nil || result.Identity == nil || result.PasswordSetup || h.identityProviders == nil {
		Unauthorized(w, "restart third-party authorization")
		return
	}
	if err := h.identityProviders.WithIdentityProvider(r.Context(), result.Provider, func(plugins.IdentityServices) error { return nil }); err != nil {
		Unauthorized(w, "provider is no longer available")
		return
	}
	h.registrationEmailCodeHandler(w, r, true)
}

const passwordSetupCookie = "zboard_oidc_password"

func (h *handlers) ExternalPasswordStatusHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	set, err := h.passwordService().HasLocalPassword(r.Context(), identity.Principal{ID: claims.UserID})
	if err != nil {
		Unauthorized(w, "account unavailable")
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": set})
}
func (h *handlers) ExternalPasswordSetupHandler(w http.ResponseWriter, r *http.Request) {
	origin, err := h.externalAuthOrigin(r.Context())
	if err != nil || !externalAuthRequest(r, origin) {
		Forbidden(w, "invalid authentication request")
		return
	}
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !pluginBody(w, r, &input) {
		return
	}
	if !validPassword(input.Password) {
		BadRequest(w, "password must be 12-72 UTF-8 bytes")
		return
	}
	proof, err := h.externalAuth.finish(cookieValue(r, origin, passwordSetupCookie))
	authCookie(w, origin, passwordSetupCookie, "", -1)
	if err != nil || !proof.PasswordSetup || proof.UserID != claims.UserID || h.identityProviders == nil {
		Unauthorized(w, "sign in again with your third-party account before setting a password")
		return
	}
	err = h.identityProviders.WithIdentityProvider(r.Context(), proof.Provider, func(services plugins.IdentityServices) error {
		return services.InitialPasswords.Setup(r.Context(), identity.Principal{ID: claims.UserID}, identity.InitialPasswordGrant{AccountID: proof.UserID, BindingID: proof.IdentityID, Authority: proof.Provider.IdentityKey(), Publisher: proof.Provider.Publisher}, input.Password)
	})
	if err != nil {
		Unauthorized(w, "password setup expired or unavailable")
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": true})
}

// Commit the attempt budget independently of a later failed user transaction.
func (h *handlers) recordExternalEmailAttempt(ctx context.Context, input externalRegistrationInput) error {
	return h.services.Identity.EmailCodes.ChargeAttempt(ctx, h.services.Identity.EmailAttempts, input.Email, input.VerificationCode)
}
