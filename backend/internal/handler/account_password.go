package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var errAccountPasswordConfirmation = errors.New("账户验证已过期，请重新确认密码。")

// Confirmation grants identity-management operations for this login only. A
// password change invalidates it without retaining plaintext credentials.
func (h *handlers) accountConfirmationMAC(user model.User, authorization, expires string) string {
	digest := sha256.Sum256([]byte(user.Password))
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	fmt.Fprintf(mac, "identity-confirmation\n%d\n%s\n%x\n%s", user.ID, authorization, digest, expires)
	return hex.EncodeToString(mac.Sum(nil))
}
func (h *handlers) accountPasswordConfirmed(user model.User, r *http.Request) bool {
	if r == nil || user.Password == "!external" {
		return false
	}
	expiry, signature, ok := strings.Cut(r.Header.Get("X-ZBoard-Account-Confirmation"), ".")
	expires, err := strconv.ParseInt(expiry, 10, 64)
	now := time.Now().Unix()
	return ok && err == nil && expires > now && expires <= now+300 && hmac.Equal([]byte(signature), []byte(h.accountConfirmationMAC(user, r.Header.Get("Authorization"), expiry)))
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
	var user model.User
	if h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error != nil {
		Unauthorized(w, "account unavailable")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)) != nil {
		BadRequest(w, "当前密码不正确，请重新输入。")
		return
	}
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
	if !validPassword(input.Password) {
		BadRequest(w, "新密码必须为 12–72 个 UTF-8 字节。")
		return
	}
	var user model.User
	if h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error != nil {
		Unauthorized(w, "account unavailable")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Current)) != nil {
		BadRequest(w, "当前密码不正确，请重新输入。")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.User{}).Where("id = ? AND password = ? AND status = ?", user.ID, user.Password, userStatusActive).Update("password", string(hash))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("account changed")
		}
		return createAuditLog(tx, claims, "account.password.change", fmt.Sprintf("user:%d", user.ID), "local password changed after password verification")
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, "账户状态已变化，请刷新重试。", nil)
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": true})
}
