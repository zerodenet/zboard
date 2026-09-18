package identity

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type RegistrationInput struct{ Email, Password, Code string }
type RegistrationResult struct {
	User      PublicAccount
	Token     string
	ExpiresAt int64
}
type RegistrationTx interface {
	RegistrationAllowed() (bool, error)
	VerificationRequired() (bool, error)
	EmailInUse(string) (bool, error)
	EmailChallenge(string) (EmailChallenge, error)
	RejectEmailChallenge(string, int, *time.Time) error
	ConsumeEmailChallenge(string, time.Time) error
	CreateLocalAccount(string, string, *time.Time) (PublicAccount, error)
	Audit(PublicAccount, string, string, string) error
}
type RegistrationRepository interface {
	WithinRegistration(context.Context, func(RegistrationTx) error) error
}
type Registration struct {
	Repository RegistrationRepository
	Tokens     SessionTokens
	EmailCodes EmailCodes
}

func (s Registration) Register(ctx context.Context, in RegistrationInput) (RegistrationResult, error) {
	in.Email, in.Code = NormalizeEmail(in.Email), strings.TrimSpace(in.Code)
	fields := map[string]string{}
	if !ValidEmail(in.Email) {
		fields["email"] = "请输入有效邮箱。"
	}
	if !ValidPassword(in.Password) {
		fields["password"] = "密码必须为 12–72 个 UTF-8 字节。"
	}
	if len(fields) > 0 {
		return RegistrationResult{}, &AccountValidation{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return RegistrationResult{}, err
	}
	var out RegistrationResult
	var rejected error
	err = s.Repository.WithinRegistration(ctx, func(tx RegistrationTx) error {
		allowed, err := tx.RegistrationAllowed()
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRegistrationClosed
		}
		required, err := tx.VerificationRequired()
		if err != nil {
			return err
		}
		now := s.Tokens.now().UTC()
		var verified *time.Time
		if required {
			if !ValidEmailCode(in.Code) {
				return &AccountValidation{Fields: map[string]string{"verification_code": "请输入 6 位邮箱验证码。"}}
			}
			challenge, err := tx.EmailChallenge(in.Email)
			if err != nil {
				return err
			}
			if challenge.Consumed || !challenge.ExpiresAt.After(now) || challenge.Attempts >= RegistrationAttemptLimit || challenge.Hash == "" {
				return &AccountValidation{Fields: map[string]string{"verification_code": "验证码已失效，请重新获取。"}}
			}
			if len(s.EmailCodes.Key) == 0 {
				return ErrVerifiedEmail
			}
			if subtle.ConstantTimeCompare([]byte(s.EmailCodes.Digest(in.Email, in.Code)), []byte(challenge.Hash)) != 1 {
				attempts := challenge.Attempts + 1
				var expires *time.Time
				if attempts >= RegistrationAttemptLimit {
					expires = &now
				}
				if err := tx.RejectEmailChallenge(in.Email, attempts, expires); err != nil {
					return err
				}
				// Commit a failed attempt without committing any account or session.
				rejected = &AccountValidation{Fields: map[string]string{"verification_code": "验证码不正确。"}}
				return nil
			}
			verified = &now
		}
		exists, err := tx.EmailInUse(in.Email)
		if err != nil {
			return err
		}
		if exists {
			return ErrEmailConflict
		}
		out.User, err = tx.CreateLocalAccount(in.Email, string(hash), verified)
		if err != nil {
			return err
		}
		if required {
			if err := tx.ConsumeEmailChallenge(in.Email, now); err != nil {
				return err
			}
		}
		out.Token, out.ExpiresAt, err = s.Tokens.Issue(SessionClaims{UserID: out.User.ID, Email: out.User.Email, IsAdmin: out.User.IsAdmin})
		if err != nil {
			return err
		}
		return tx.Audit(out.User, "account.register", in.Email, "core registered an ordinary local account")
	})
	if err != nil {
		return RegistrationResult{}, err
	}
	if rejected != nil {
		return RegistrationResult{}, rejected
	}
	return out, nil
}
