package handler

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type identityRuntime interface {
	IdentityProviders() ([]plugins.IdentityProviderView, error)
	IdentityProvider(context.Context, string) (plugins.IdentitySnapshot, error)
	ExchangeIdentity(context.Context, plugins.IdentitySnapshot, *pluginv1.IdentityExchange, func(*pluginv1.VerifiedIdentity, *gorm.DB) error) error
	WithIdentityProvider(plugins.IdentitySnapshot, func(*gorm.DB) error) error
}

const externalAuthPath = "/api/v1/auth/oidc"
const flowCookie = "zboard_oidc_flow"
const resultCookie = "zboard_oidc_result"

func (h *handlers) externalAuthOrigin() (string, error) {
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimRight(installation.SiteURL, "/"))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return "", errors.New("configure a canonical site origin before enabling third-party login")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("third-party login requires HTTPS")
	}
	return u.String(), nil
}
func externalAuthRequest(r *http.Request, origin string) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && media == "application/json" && r.Header.Get("Sec-Fetch-Site") != "cross-site" && (r.Header.Get("Origin") == "" || r.Header.Get("Origin") == origin)
}
func authCookieName(origin, name string) string {
	if strings.HasPrefix(origin, "https://") {
		return "__Host-" + name
	}
	return name
}
func authCookie(w http.ResponseWriter, origin, name, value string, maxAge int) {
	secure := strings.HasPrefix(origin, "https://")
	path := externalAuthPath
	if secure {
		path = "/"
	} // __Host- forbids Domain and requires Secure + Path=/.
	http.SetCookie(w, &http.Cookie{Name: authCookieName(origin, name), Value: value, Path: path, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func cookieValue(r *http.Request, origin, name string) string {
	c, err := r.Cookie(authCookieName(origin, name))
	if err != nil {
		return ""
	}
	return c.Value
}
func authNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
func (h *handlers) ExternalAuthProvidersHandler(w http.ResponseWriter, r *http.Request) {
	authNoStore(w)
	out := []plugins.IdentityProviderView{}
	if h.identityProviders != nil {
		if values, err := h.identityProviders.IdentityProviders(); err == nil {
			out = values
		}
	}
	OK(w, out)
}
func (h *handlers) ExternalAuthStartHandler(w http.ResponseWriter, r *http.Request) {
	h.startExternalAuth(w, r, false)
}
func (h *handlers) ExternalIdentityBindHandler(w http.ResponseWriter, r *http.Request) {
	h.startExternalAuth(w, r, true)
}
func (h *handlers) startExternalAuth(w http.ResponseWriter, r *http.Request, bind bool) {
	authNoStore(w)
	if h.identityProviders == nil {
		ServiceUnavailable(w, "identity provider unavailable")
		return
	}
	origin, err := h.externalAuthOrigin()
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if !externalAuthRequest(r, origin) {
		Forbidden(w, "invalid authentication origin or content type")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	flow := externalAuthFlow{}
	if bind {
		claims, err := h.authFromRequest(r)
		if err != nil {
			Unauthorized(w, "authentication required")
			return
		}
		var user model.User
		if err := h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)) != nil {
			Unauthorized(w, "confirm your current account password before linking")
			return
		}
		flow.BindUserID = user.ID
		flow.PasswordHash = user.Password
	}
	provider, err := h.identityProviders.IdentityProvider(r.Context(), pathvar.Vars(r)["id"])
	if err != nil {
		BadRequest(w, "identity provider is unavailable; review its configuration")
		return
	}
	flow.Provider = provider
	flow.RedirectURI = origin + externalAuthPath + "/callback"
	if flow.Binding, err = authRandom(); err != nil {
		ServerError(w, err)
		return
	}
	if flow.Nonce, err = authRandom(); err != nil {
		ServerError(w, err)
		return
	}
	if flow.Verifier, err = authRandom(); err != nil {
		ServerError(w, err)
		return
	}
	state, err := h.externalAuth.add(flow, cookieValue(r, origin, flowCookie))
	if err != nil {
		ServiceUnavailable(w, err.Error())
		return
	}
	authorization, _ := url.Parse(provider.Provider.AuthorizationEndpoint)
	query := authorization.Query()
	query.Set("response_type", "code")
	query.Set("client_id", provider.Provider.ClientId)
	query.Set("redirect_uri", flow.RedirectURI)
	query.Set("scope", strings.Join(provider.Provider.Scopes, " "))
	query.Set("state", state)
	query.Set("nonce", flow.Nonce)
	query.Set("code_challenge", pkceChallenge(flow.Verifier))
	query.Set("code_challenge_method", "S256")
	authorization.RawQuery = query.Encode()
	authCookie(w, origin, flowCookie, flow.Binding, 300)
	// A previous completion cannot be redeemed as the result of this new attempt.
	authCookie(w, origin, resultCookie, "", -1)
	OK(w, map[string]string{"authorization_url": authorization.String()})
}
func (h *handlers) ExternalAuthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	authNoStore(w)
	origin, err := h.externalAuthOrigin()
	if err != nil {
		BadRequest(w, "identity callback is unavailable")
		return
	}
	fail := func() {
		authCookie(w, origin, resultCookie, "", -1)
		http.Redirect(w, r, origin+"/auth/oidc/complete?error=failed", http.StatusSeeOther)
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["state"]) != 1 || len(query["code"]) > 1 || len(query["iss"]) > 1 || len(query["error"]) > 1 {
		fail()
		return
	}
	flow, err := h.externalAuth.take(query.Get("state"), cookieValue(r, origin, flowCookie))
	if err != nil {
		fail()
		return
	}
	authCookie(w, origin, flowCookie, "", -1)
	code := query.Get("code")
	if query.Get("error") != "" || len(code) == 0 || len(code) > 8192 || h.identityProviders == nil || flow.RedirectURI != origin+externalAuthPath+"/callback" {
		fail()
		return
	}
	if issuer := query.Get("iss"); issuer != "" && issuer != flow.Provider.Provider.Issuer {
		fail()
		return
	}
	result := externalAuthCompletion{Provider: flow.Provider}
	err = h.identityProviders.ExchangeIdentity(r.Context(), flow.Provider, &pluginv1.IdentityExchange{Code: code, RedirectUri: flow.RedirectURI, Nonce: flow.Nonce, PkceVerifier: flow.Verifier, Issuer: flow.Provider.Provider.Issuer, ProviderId: flow.Provider.Provider.ProviderId}, func(identity *pluginv1.VerifiedIdentity, tx *gorm.DB) error {
		return h.resolveExternalIdentity(tx, flow, identity, &result)
	})
	if err != nil {
		fail()
		return
	}
	ticket, err := h.externalAuth.complete(result)
	if err != nil {
		fail()
		return
	}
	authCookie(w, origin, resultCookie, ticket, 60)
	http.Redirect(w, r, origin+"/auth/oidc/complete", http.StatusSeeOther)
}
func (h *handlers) ExternalAuthFinishHandler(w http.ResponseWriter, r *http.Request) {
	authNoStore(w)
	origin, err := h.externalAuthOrigin()
	if err != nil {
		BadRequest(w, "authentication unavailable")
		return
	}
	if !externalAuthRequest(r, origin) {
		Forbidden(w, "invalid authentication origin or content type")
		return
	}
	var input externalRegistrationInput
	if !pluginBody(w, r, &input) {
		return
	}
	result, err := h.externalAuth.finish(cookieValue(r, origin, resultCookie))
	authCookie(w, origin, resultCookie, "", -1)
	if err != nil || result.PasswordSetup || h.identityProviders == nil {
		Unauthorized(w, "login expired or failed; restart third-party authorization")
		return
	}
	if result.Identity != nil && (!result.Identity.EmailVerified || !validEmail(normalizeEmail(result.Identity.Email))) {
		if input.Email == "" && input.VerificationCode == "" {
			if err := h.identityProviders.WithIdentityProvider(result.Provider, func(tx *gorm.DB) error {
				var installation model.Installation
				if err := tx.First(&installation, 1).Error; err != nil {
					return err
				}
				if !installation.AllowRegistration {
					return errors.New("registration disabled")
				}
				return nil
			}); err != nil {
				Forbidden(w, "registration unavailable")
				return
			}
			ticket, err := h.externalAuth.complete(result)
			if err != nil {
				ServiceUnavailable(w, "registration unavailable")
				return
			}
			authCookie(w, origin, resultCookie, ticket, 300)
			OK(w, map[string]any{"registration_required": true, "email": result.Identity.Email})
			return
		}
		if err := h.recordExternalEmailAttempt(input); err != nil {
			Unauthorized(w, "email verification failed; restart authorization")
			return
		}
	}
	var output any
	err = h.identityProviders.WithIdentityProvider(result.Provider, func(tx *gorm.DB) error {
		var err error
		output, err = h.finishExternalIdentityRegistration(tx, result, input)
		return err
	})
	if err != nil {
		Unauthorized(w, "identity or account is no longer available")
		return
	}
	if response, ok := output.(map[string]any); ok {
		if public, ok := response["user"].(userPublic); ok {
			var user model.User
			if h.db.First(&user, public.ID).Error == nil {
				if result.Identity != nil {
					_ = h.enqueueRegistrationWelcome(user)
				}
				if user.Password == "!external" {
					identityID := result.IdentityID
					if result.Identity != nil {
						identityID = externalIdentityID(result.Provider, result.Identity)
					}
					proof, err := h.externalAuth.complete(externalAuthCompletion{Provider: result.Provider, UserID: user.ID, IdentityID: identityID, PasswordSetup: true})
					if err == nil {
						authCookie(w, origin, passwordSetupCookie, proof, 300)
					}
				}
			}
		}
	}
	OK(w, output)
}
