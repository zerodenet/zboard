package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	registrationChallengePurpose  = "register"
	registrationCodeTTL           = 10 * time.Minute
	registrationCodeCooldown      = 60 * time.Second
	registrationCodeMaxAttempts   = 5
	registrationCodeIPHourlyLimit = 20
)

var registrationCodePattern = regexp.MustCompile(`^\d{6}$`)

type registrationCodeRequest struct {
	Email string `json:"email"`
}

func (h *handlers) RegistrationEmailCodeHandler(w http.ResponseWriter, r *http.Request) {
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		ServerError(w, err)
		return
	}
	if !installation.AllowRegistration {
		Forbidden(w, "public registration is disabled")
		return
	}
	enabled, err := h.registrationEmailVerificationEnabled(h.db)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !enabled {
		BadRequest(w, "registration email verification is disabled")
		return
	}
	var body registrationCodeRequest
	if err := decodeBody(r, &body); err != nil {
		BadRequest(w, err.Error())
		return
	}
	body.Email = normalizeEmail(body.Email)
	if !validEmail(body.Email) {
		BadRequestFields(w, "验证码请求校验失败。", map[string]string{"email": "请输入有效邮箱。"})
		return
	}
	var existingUsers int64
	// Soft-deleted identities still own their unique email address. Reject them
	// here so we never deliver a code that registration cannot consume.
	if err := h.db.Unscoped().Model(&model.User{}).Where("email = ?", body.Email).Count(&existingUsers).Error; err != nil {
		ServerError(w, err)
		return
	}
	if existingUsers > 0 {
		BadRequestFields(w, "验证码请求校验失败。", map[string]string{"email": "该邮箱已存在。"})
		return
	}
	settings, err := h.loadSMTPSettings(h.db, false)
	if err == nil {
		err = validateSMTPDeliverySettings(settings, false)
	}
	if err != nil {
		ServiceUnavailable(w, "registration email delivery is not configured")
		return
	}
	code, err := secureRegistrationCode()
	if err != nil {
		ServerError(w, err)
		return
	}
	now := time.Now().UTC()
	ipHash := h.registrationRequestIPHash(r.RemoteAddr)
	challenge := model.RegistrationEmailChallenge{}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var recent int64
		if err := tx.Model(&model.RegistrationEmailChallenge{}).
			Where("requested_ip_hash = ? AND updated_at > ?", ipHash, now.Add(-time.Hour)).Count(&recent).Error; err != nil {
			return err
		}
		if recent >= registrationCodeIPHourlyLimit {
			return errRegistrationCodeRateLimited
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("email = ? AND purpose = ?", body.Email, registrationChallengePurpose).First(&challenge)
		if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if lookup.Error == nil && challenge.LastSentAt.Add(registrationCodeCooldown).After(now) {
			return errRegistrationCodeCooldown
		}
		challenge.Email = body.Email
		challenge.Purpose = registrationChallengePurpose
		challenge.CodeHash = h.registrationCodeDigest(body.Email, code)
		challenge.RequestedIPHash = ipHash
		challenge.Attempts = 0
		challenge.LastSentAt = now
		challenge.ExpiresAt = now.Add(registrationCodeTTL)
		challenge.ConsumedAt = nil
		if challenge.ID == 0 {
			return tx.Create(&challenge).Error
		}
		return tx.Save(&challenge).Error
	})
	if errors.Is(err, errRegistrationCodeCooldown) {
		w.Header().Set("Retry-After", strconv.Itoa(int(registrationCodeCooldown.Seconds())))
		writeJSON(w, http.StatusTooManyRequests, "验证码发送过于频繁，请一分钟后重试。", map[string]int{"retry_after": int(registrationCodeCooldown.Seconds())})
		return
	}
	if errors.Is(err, errRegistrationCodeRateLimited) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, "当前网络的验证码请求过多，请稍后重试。", map[string]int{"retry_after": 3600})
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	siteName, siteURL, _ := h.loadEmailSiteIdentity()
	if siteName == "" {
		siteName = "Zboard"
	}
	subject := siteName + " 注册验证码"
	bodyText := fmt.Sprintf("你的注册验证码是：%s\n\n验证码在 10 分钟内有效，请勿转发给他人。\n\n访问地址：%s", code, siteURL)
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := sendSMTPMail(ctx, settings, body.Email, subject, bodyText, fmt.Sprintf("<registration-code-%s@%s>", uuid.NewString(), settings.Host)); err != nil {
		_ = h.db.Model(&model.RegistrationEmailChallenge{}).Where("id = ?", challenge.ID).Updates(map[string]interface{}{
			"code_hash": "", "expires_at": now, "last_sent_at": now.Add(-registrationCodeCooldown),
		}).Error
		ServiceUnavailable(w, "registration verification email could not be delivered")
		return
	}
	OK(w, map[string]interface{}{
		"sent": true, "expires_in": int(registrationCodeTTL.Seconds()), "resend_after": int(registrationCodeCooldown.Seconds()),
	})
}

func (h *handlers) registrationEmailVerificationEnabled(db *gorm.DB) (bool, error) {
	var config model.SystemConfig
	err := db.Where("config_key = ?", "register_email_verification").First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(strings.TrimSpace(config.Value))
}

func secureRegistrationCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func (h *handlers) registrationCodeDigest(email, code string) string {
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	_, _ = mac.Write([]byte(registrationChallengePurpose + "\x00" + normalizeEmail(email) + "\x00" + code))
	return hex.EncodeToString(mac.Sum(nil))
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

func (h *handlers) createVerifiedRegistrationUser(user *model.User, code string) error {
	now := time.Now().UTC()
	var verificationErr error
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var challenge model.RegistrationEmailChallenge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("email = ? AND purpose = ?", user.Email, registrationChallengePurpose).First(&challenge).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				verificationErr = validationError("注册信息校验失败。", map[string]string{"verification_code": "请先获取邮箱验证码。"})
				return nil
			}
			return err
		}
		if challenge.ConsumedAt != nil || !challenge.ExpiresAt.After(now) || challenge.Attempts >= registrationCodeMaxAttempts || challenge.CodeHash == "" {
			verificationErr = validationError("注册信息校验失败。", map[string]string{"verification_code": "验证码已失效，请重新获取。"})
			return nil
		}
		expected := h.registrationCodeDigest(user.Email, code)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge.CodeHash)) != 1 {
			challenge.Attempts++
			if challenge.Attempts >= registrationCodeMaxAttempts {
				challenge.ExpiresAt = now
			}
			if err := tx.Save(&challenge).Error; err != nil {
				return err
			}
			verificationErr = validationError("注册信息校验失败。", map[string]string{"verification_code": "验证码不正确。"})
			return nil
		}
		user.EmailVerifiedAt = &now
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		challenge.ConsumedAt = &now
		return tx.Save(&challenge).Error
	})
	if err != nil {
		return err
	}
	return verificationErr
}

var (
	errRegistrationCodeCooldown    = errors.New("registration verification code cooldown")
	errRegistrationCodeRateLimited = errors.New("registration verification code rate limited")
)
