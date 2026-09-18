package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

func (h *handlers) accountService() identity.Accounts {
	return h.services.Identity.Accounts
}
func (h *handlers) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	fields := map[string]string{}
	if identity.NormalizeEmail(body.Email) == "" {
		fields["email"] = "请输入邮箱地址。"
	}
	if body.Password == "" {
		fields["password"] = "请输入密码。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "登录信息不完整。", fields)
		return
	}
	user, err := h.accountService().Login(r.Context(), body.Email, body.Password)
	if errors.Is(err, identity.ErrCredentials) {
		Unauthorized(w, "invalid email or password")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	token, expires, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]any{"user": user, "auth": tokenResponse{Token: token, ExpiresAt: expires}})
}
func (h *handlers) MeHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	user, err := h.accountService().Me(r.Context(), identity.Principal{ID: claims.UserID, Actor: claims.Email})
	if errors.Is(err, identity.ErrUnavailable) {
		Unauthorized(w, "user not found")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, user)
}

func (h *handlers) sessionTokens() identity.SessionTokens {
	return h.services.Identity.Tokens
}
func (h *handlers) issueToken(claims authClaims) (string, int64, error) {
	return h.sessionTokens().Issue(claims)
}
func (h *handlers) sign(payload []byte) []byte { return h.sessionTokens().Sign(payload) }
func (h *handlers) authFromRequest(r *http.Request) (authClaims, error) {
	raw := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = raw[len("bearer "):]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return authClaims{}, errors.New("authorization required")
	}
	service := h.services.Identity.Sessions
	claims, err := service.Authenticate(r.Context(), raw)
	if errors.Is(err, identity.ErrUnavailable) {
		return authClaims{}, errors.New("user not found")
	}
	if err != nil && !errors.Is(err, identity.ErrToken) && !errors.Is(err, identity.ErrExpired) {
		return authClaims{}, errors.New("token validation failed")
	}
	return claims, err
}
