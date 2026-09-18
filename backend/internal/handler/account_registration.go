package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

func (h *handlers) RegisterAuthRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"verification_code"`
	}
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	service := h.services.Identity.Registration
	result, err := service.Register(r.Context(), identity.RegistrationInput{Email: body.Email, Password: body.Password, Code: body.Code})
	if err != nil {
		var validation *identity.AccountValidation
		switch {
		case errors.As(err, &validation):
			BadRequestFields(w, "注册信息校验失败。", validation.Fields)
		case errors.Is(err, identity.ErrRegistrationClosed):
			Forbidden(w, "public registration is disabled")
		case errors.Is(err, identity.ErrEmailConflict):
			BadRequestFields(w, "注册信息校验失败。", map[string]string{"email": "该邮箱已存在。"})
		default:
			ServerError(w, err)
		}
		return
	}
	// The account transaction persisted its registration event for messaging.
	OK(w, map[string]any{"user": result.User, "auth": tokenResponse{Token: result.Token, ExpiresAt: result.ExpiresAt}})
}
