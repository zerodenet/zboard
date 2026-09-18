package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const RegistrationPurpose = "register"
const RegistrationAttemptLimit = 5

type EmailCodes struct{ Key []byte }

func (c EmailCodes) Digest(email, code string) string {
	mac := hmac.New(sha256.New, c.Key)
	_, _ = mac.Write([]byte(RegistrationPurpose + "\x00" + NormalizeEmail(email) + "\x00" + code))
	return hex.EncodeToString(mac.Sum(nil))
}
func ValidEmailCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func (s ExternalIdentities) register(tx ExternalTx, e ExternalEvidence, input RegistrationEmail) (PublicAccount, ExternalBinding, error) {
	var empty PublicAccount
	var none ExternalBinding
	allowed, err := tx.RegistrationAllowed()
	if err != nil {
		return empty, none, err
	}
	if !allowed {
		return empty, none, ErrRegistrationClosed
	}
	binding, found, err := tx.FindBinding(e.Key())
	if err != nil {
		return empty, none, err
	}
	if found {
		user, err := tx.Account(binding.UserID, nil)
		return user.PublicAccount, binding, err
	}
	email := NormalizeEmail(e.Email)
	now := s.Tokens.now().UTC()
	if !e.EmailVerified || !ValidEmail(email) {
		email = NormalizeEmail(input.Email)
		code := strings.TrimSpace(input.Code)
		if !ValidEmail(email) || !ValidEmailCode(code) {
			return empty, none, ErrVerifiedEmail
		}
		challenge, err := tx.EmailChallenge(email)
		if err != nil {
			return empty, none, ErrVerifiedEmail
		}
		expected := s.EmailCodes.Digest(email, code)
		if len(s.EmailCodes.Key) == 0 || challenge.Consumed || !challenge.ExpiresAt.After(now) || challenge.Attempts > RegistrationAttemptLimit || subtle.ConstantTimeCompare([]byte(expected), []byte(challenge.Hash)) != 1 {
			return empty, none, ErrVerifiedEmail
		}
		if err := tx.ConsumeEmailChallenge(email, now); err != nil {
			return empty, none, err
		}
	}
	exists, err := tx.EmailInUse(email)
	if err != nil {
		return empty, none, err
	}
	if exists {
		return empty, none, errors.New("email already belongs to an account; sign in and explicitly link this provider")
	}
	user, err := tx.CreateExternalAccount(email, now)
	if err != nil {
		return empty, none, err
	}
	binding = ExternalBinding{ID: e.Key(), UserID: user.ID, Authority: e.Authority, Publisher: e.Publisher, Issuer: e.Issuer, Subject: e.Subject}
	if err := tx.CreateBinding(binding); err != nil {
		return empty, none, err
	}
	err = tx.Audit(user, "plugin.identity.register", e.Authority, "plugin identity verified; core registered an ordinary user")
	return user, binding, err
}

// Attempt charging is committed before the provider-guarded registration
// transaction so a failed account creation cannot restore brute-force budget.
type RegistrationAttempts interface {
	ChargeRegistrationAttempt(context.Context, string, time.Time, int) (bool, error)
}

func (c EmailCodes) ChargeAttempt(ctx context.Context, repo RegistrationAttempts, email, code string) error {
	email = NormalizeEmail(email)
	code = strings.TrimSpace(code)
	if !ValidEmail(email) || !ValidEmailCode(code) {
		return ErrVerifiedEmail
	}
	charged, err := repo.ChargeRegistrationAttempt(ctx, email, time.Now().UTC(), RegistrationAttemptLimit)
	if err != nil {
		return err
	}
	if !charged {
		return ErrVerifiedEmail
	}
	return nil
}
