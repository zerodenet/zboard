package handler

import (
	"crypto/subtle"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"strings"
	"time"
)

type externalRegistrationInput struct {
	Email            string `json:"email"`
	VerificationCode string `json:"verification_code"`
}

var errExternalRegistrationEmail = errors.New("verify an email address to complete registration")

// Only core creates a user, with its registration switch, uniqueness, ordinary
// role, verified email, identity binding and audit in the same transaction.
func (h *handlers) createExternalRegistration(tx *gorm.DB, result externalAuthCompletion, input externalRegistrationInput) (model.User, model.ExternalIdentity, error) {
	var user model.User
	var binding model.ExternalIdentity
	var installation model.Installation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error; err != nil {
		return user, binding, err
	}
	if !installation.AllowRegistration {
		return user, binding, errors.New("public registration is disabled")
	}
	identity := result.Identity
	bindingID := externalIdentityID(result.Provider, identity)
	// An overlapping successful flow can only resolve the same identity, never
	// claim a different account based on an email match.
	if err := tx.Where("id = ?", bindingID).First(&binding).Error; err == nil {
		err = tx.Where("id = ? AND status = ?", binding.UserID, userStatusActive).First(&user).Error
		return user, binding, err
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return user, binding, err
	}
	email := normalizeEmail(identity.Email)
	if !identity.EmailVerified || !validEmail(email) {
		email = normalizeEmail(input.Email)
		if !validEmail(email) || !registrationCodePattern.MatchString(strings.TrimSpace(input.VerificationCode)) {
			return user, binding, errExternalRegistrationEmail
		}
		var challenge model.RegistrationEmailChallenge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ? AND purpose = ?", email, registrationChallengePurpose).First(&challenge).Error; err != nil {
			return user, binding, errExternalRegistrationEmail
		}
		expected := h.registrationCodeDigest(email, strings.TrimSpace(input.VerificationCode))
		if challenge.ConsumedAt != nil || !challenge.ExpiresAt.After(time.Now()) || challenge.Attempts > registrationCodeMaxAttempts || subtle.ConstantTimeCompare([]byte(expected), []byte(challenge.CodeHash)) != 1 {
			return user, binding, errExternalRegistrationEmail
		}
		now := time.Now().UTC()
		challenge.ConsumedAt = &now
		if err := tx.Save(&challenge).Error; err != nil {
			return user, binding, err
		}
	}
	var count int64
	if err := tx.Unscoped().Model(&model.User{}).Where("email = ?", email).Count(&count).Error; err != nil {
		return user, binding, err
	}
	if count != 0 {
		return user, binding, errors.New("email already belongs to an account; sign in and explicitly link this provider")
	}
	now := time.Now().UTC()
	// An explicit non-bcrypt sentinel disables password authentication until the
	// user chooses a password using a short-lived core grant from OAuth login.
	user = model.User{AccountName: email, Email: email, Password: "!external", IsAdmin: false, Status: userStatusActive, EmailVerifiedAt: &now}
	if err := tx.Create(&user).Error; err != nil {
		return user, binding, err
	}
	binding = model.ExternalIdentity{ID: bindingID, UserID: user.ID, PluginID: result.Provider.IdentityKey(), Publisher: result.Provider.Publisher, Issuer: identity.Issuer, Subject: identity.Subject}
	if err := tx.Create(&binding).Error; err != nil {
		return user, binding, err
	}
	err := createAuditLog(tx, authClaims{UserID: user.ID, Email: user.Email}, "plugin.identity.register", result.Provider.IdentityKey(), "plugin identity verified; core registered an ordinary user")
	return user, binding, err
}

func (h *handlers) ExternalRegistrationCodeHandler(w http.ResponseWriter, r *http.Request) {
	origin, err := h.externalAuthOrigin()
	if err != nil || !externalAuthRequest(r, origin) {
		Forbidden(w, "invalid authentication request")
		return
	}
	result, err := h.externalAuth.peek(cookieValue(r, origin, resultCookie))
	if err != nil || result.Identity == nil || result.PasswordSetup || h.identityProviders == nil {
		Unauthorized(w, "restart third-party authorization")
		return
	}
	if err := h.identityProviders.WithIdentityProvider(result.Provider, func(*gorm.DB) error { return nil }); err != nil {
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
	var user model.User
	if h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error != nil {
		Unauthorized(w, "account unavailable")
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": user.Password != "!external"})
}
func (h *handlers) ExternalPasswordSetupHandler(w http.ResponseWriter, r *http.Request) {
	origin, err := h.externalAuthOrigin()
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
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		ServerError(w, err)
		return
	}
	err = h.identityProviders.WithIdentityProvider(proof.Provider, func(tx *gorm.DB) error {
		var binding model.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ?", proof.IdentityID, claims.UserID).First(&binding).Error; err != nil {
			return err
		}
		update := tx.Model(&model.User{}).Where("id = ? AND status = ? AND password = ?", claims.UserID, userStatusActive, "!external").Update("password", string(hash))
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errors.New("password already set or account unavailable")
		}
		return createAuditLog(tx, claims, "identity.password.setup", proof.Provider.IdentityKey(), "initial password set after recent external authentication")
	})
	if err != nil {
		Unauthorized(w, "password setup expired or unavailable")
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"password_set": true})
}

// Commit the attempt budget independently of a later failed user transaction.
func (h *handlers) recordExternalEmailAttempt(input externalRegistrationInput) error {
	email := normalizeEmail(input.Email)
	if !validEmail(email) || !registrationCodePattern.MatchString(strings.TrimSpace(input.VerificationCode)) {
		return errExternalRegistrationEmail
	}
	result := h.db.Model(&model.RegistrationEmailChallenge{}).Where("email = ? AND purpose = ? AND attempts < ? AND consumed_at IS NULL AND expires_at > ?", email, registrationChallengePurpose, registrationCodeMaxAttempts, time.Now()).Update("attempts", gorm.Expr("attempts + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errExternalRegistrationEmail
	}
	return nil
}
