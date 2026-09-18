package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

const (
	registrationChallengePurpose = identity.RegistrationPurpose
	registrationCodeMaxAttempts  = identity.RegistrationAttemptLimit
)

var registrationCodePattern = regexp.MustCompile(`^\d{6}$`)

type registrationCodeRequest struct {
	Email string `json:"email"`
}

func (h *handlers) RegistrationEmailCodeHandler(w http.ResponseWriter, r *http.Request) {
	h.registrationEmailCodeHandler(w, r, false)
}

type registrationCodeDelivery func(context.Context, string, string) error

func (f registrationCodeDelivery) SendRegistrationCode(ctx context.Context, email, code string) error {
	return f(ctx, email, code)
}

func (h *handlers) registrationEmailCodeHandler(w http.ResponseWriter, r *http.Request, external bool) {
	var body registrationCodeRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	settings, err := h.services.SMTPSettings(r.Context(), h.credentialCipher, false)
	if err == nil {
		err = validateSMTPDeliverySettings(settings, false)
	}
	if err != nil {
		ServiceUnavailable(w, "registration email delivery is not configured")
		return
	}
	service := h.services.Identity.CodeIssuance(registrationCodeDelivery(func(ctx context.Context, email, code string) error {
		siteName, siteURL, _ := h.loadEmailSiteIdentity(ctx)
		if siteName == "" {
			siteName = "Zboard"
		}
		subject := siteName + " 注册验证码"
		bodyText := fmt.Sprintf("你的注册验证码是：%s\n\n验证码在 10 分钟内有效，请勿转发给他人。\n\n访问地址：%s", code, siteURL)
		return sendSMTPMail(ctx, settings, email, subject, bodyText, fmt.Sprintf("<registration-code-%s@%s>", uuid.NewString(), settings.Host))
	}))
	err = service.Send(r.Context(), identity.CodeRequest{Email: body.Email, SourceHash: h.registrationRequestIPHash(r.RemoteAddr), External: external})
	var validation *identity.AccountValidation
	switch {
	case err == nil:
		OK(w, map[string]any{"sent": true, "expires_in": int(identity.CodeLifetime.Seconds()), "resend_after": int(identity.CodeCooldown.Seconds())})
	case errors.As(err, &validation):
		BadRequestFields(w, "验证码请求校验失败。", validation.Fields)
	case errors.Is(err, identity.ErrRegistrationClosed):
		Forbidden(w, "public registration is disabled")
	case errors.Is(err, identity.ErrCodeDisabled):
		BadRequest(w, "registration email verification is disabled")
	case errors.Is(err, identity.ErrEmailConflict):
		BadRequestFields(w, "验证码请求校验失败。", map[string]string{"email": "该邮箱已存在。"})
	case errors.Is(err, identity.ErrCodeCooldown):
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, "验证码发送过于频繁，请一分钟后重试。", map[string]int{"retry_after": 60})
	case errors.Is(err, identity.ErrCodeRateLimited):
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, "当前网络的验证码请求过多，请稍后重试。", map[string]int{"retry_after": 3600})
	case errors.Is(err, identity.ErrCodeDelivery):
		ServiceUnavailable(w, "registration verification email could not be delivered")
	default:
		ServerError(w, err)
	}
}

func secureRegistrationCode() (string, error) { return identity.GenerateEmailCode() }

func (h *handlers) registrationCodeDigest(email, code string) string {
	return (identity.EmailCodes{Key: []byte(h.jwtSecret)}).Digest(email, code)
}

func (h *handlers) registrationRequestIPHash(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	_, _ = mac.Write([]byte("registration-code-ip\x00" + host))
	return hex.EncodeToString(mac.Sum(nil))
}
