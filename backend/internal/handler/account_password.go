package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

var errAccountPasswordConfirmation = errors.New("账户验证已过期，请重新确认密码。")

// Confirmation grants identity-management operations for this login only. A
// password change invalidates it without retaining plaintext credentials.
func (h *handlers) accountConfirmationMAC(user model.User, authorization, expires string) string {
	return (identity.PasswordConfirmation{Key: []byte(h.jwtSecret)}).Digest(user.ID, user.Password, authorization, expires)
}
func (h *handlers) accountPasswordConfirmed(user model.User, r *http.Request) bool {
	if r == nil {
		return false
	}
	return (identity.PasswordConfirmation{Key: []byte(h.jwtSecret)}).Valid(identity.Account{ID: user.ID, PasswordHash: user.Password}, r.Header.Get("Authorization"), r.Header.Get("X-ZBoard-Account-Confirmation"))
}

func (h *handlers) ConfirmAccountPasswordHandler(w http.ResponseWriter, r *http.Request) {
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
	account, err := h.passwordService().Verify(r.Context(), identity.Principal{ID: claims.UserID, Actor: claims.Email}, input.Password)
	if err != nil {
		writePasswordError(w, err)
		return
	}
	user := model.User{ID: account.ID, Password: account.PasswordHash}
	expiry := strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)
	authNoStore(w)
	OK(w, map[string]any{"confirmation": expiry + "." + h.accountConfirmationMAC(user, r.Header.Get("Authorization"), expiry), "expires_at": expiry})
}
func (h *handlers) ChangeAccountPasswordHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	var input struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !pluginBody(w, r, &input) {
		return
	}
	if err := h.passwordService().Change(r.Context(), identity.Principal{ID: claims.UserID, Actor: claims.Email}, input.Current, input.Password); err != nil {
		writePasswordError(w, err)
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": true})
}

func (h *handlers) passwordService() identity.PasswordService {
	return h.services.Identity.Passwords
}
func writePasswordError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrPasswordPolicy):
		BadRequest(w, "新密码必须为 12–72 个 UTF-8 字节。")
	case errors.Is(err, identity.ErrPassword):
		BadRequest(w, "当前密码不正确，请重新输入。")
	case errors.Is(err, identity.ErrUnavailable):
		Unauthorized(w, "account unavailable")
	case errors.Is(err, identity.ErrConflict):
		writeJSON(w, http.StatusConflict, "账户状态已变化，请刷新重试。", nil)
	default:
		ServerError(w, err)
	}
}
