package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrRegistrationClosed = errors.New("public registration is disabled")
var ErrBindingConflict = errors.New("identity is linked to another account")
var ErrVerifiedEmail = errors.New("verify an email address to complete registration")

// Evidence comes from an admitted provider adapter after protocol verification.
// Authority and publisher scope identity independently of any matching email.
type ExternalEvidence struct {
	Authority, Publisher, Issuer, Subject, Email string
	EmailVerified                                bool
}

func (e ExternalEvidence) Key() string {
	payload, _ := json.Marshal([]string{e.Publisher, e.Authority, e.Issuer, e.Subject})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
func (e ExternalEvidence) valid() bool {
	return e.Authority != "" && e.Publisher != "" && e.Issuer != "" && e.Subject != ""
}

type ExternalBinding struct {
	ID                                    string
	UserID                                uint
	Authority, Publisher, Issuer, Subject string
	CreatedAt                             time.Time
}
type BindingConfirmation struct {
	AccountID    uint
	PasswordHash string
}
type ExternalResolution struct {
	UserID                       uint
	BindingID                    string
	Linked, RegistrationRequired bool
}
type ExternalCompletion struct {
	Authority, Publisher string
	UserID               uint
	BindingID            string
	Linked               bool
	Evidence             *ExternalEvidence
}
type RegistrationEmail struct{ Email, Code string }
type ExternalFinish struct {
	User      PublicAccount
	Token     string
	ExpiresAt int64
	Linked    bool
}
type EmailChallenge struct {
	Hash      string
	ExpiresAt time.Time
	Attempts  int
	Consumed  bool
}

type ExternalTx interface {
	FindBinding(string) (ExternalBinding, bool, error)
	Account(uint, *string) (LoginAccount, error)
	RegistrationAllowed() (bool, error)
	CreateBinding(ExternalBinding) error
	CreateExternalAccount(string, time.Time) (PublicAccount, error)
	EmailInUse(string) (bool, error)
	EmailChallenge(string) (EmailChallenge, error)
	ConsumeEmailChallenge(string, time.Time) error
	TouchLogin(uint, time.Time) error
	Audit(PublicAccount, string, string, string) error
}
type ExternalRepository interface {
	WithinExternal(context.Context, func(ExternalTx) error) error
}
type ExternalIdentities struct {
	Repository ExternalRepository
	Tokens     SessionTokens
	EmailCodes EmailCodes
}

func (s ExternalIdentities) Resolve(ctx context.Context, e ExternalEvidence, confirmation BindingConfirmation) (ExternalResolution, error) {
	if !e.valid() {
		return ExternalResolution{}, ErrUnavailable
	}
	var result ExternalResolution
	err := s.Repository.WithinExternal(ctx, func(tx ExternalTx) error {
		binding, found, err := tx.FindBinding(e.Key())
		if err != nil {
			return err
		}
		if confirmation.AccountID == 0 {
			if !found {
				allowed, err := tx.RegistrationAllowed()
				if err != nil {
					return err
				}
				if !allowed {
					return ErrRegistrationClosed
				}
				result.RegistrationRequired = true
				return nil
			}
			user, err := tx.Account(binding.UserID, nil)
			if err != nil {
				return err
			}
			result.UserID = user.ID
			result.BindingID = binding.ID
			return nil
		}
		if confirmation.PasswordHash == "" || confirmation.PasswordHash == "!external" {
			return ErrPassword
		}
		user, err := tx.Account(confirmation.AccountID, &confirmation.PasswordHash)
		if err != nil {
			return err
		}
		if found && binding.UserID != user.ID {
			return ErrBindingConflict
		}
		if !found {
			binding = ExternalBinding{ID: e.Key(), UserID: user.ID, Authority: e.Authority, Publisher: e.Publisher, Issuer: e.Issuer, Subject: e.Subject}
			if err := tx.CreateBinding(binding); err != nil {
				return err
			}
			if err := tx.Audit(user.PublicAccount, "plugin.identity.link", e.Authority, "plugin identity linked after core password confirmation"); err != nil {
				return err
			}
		}
		result = ExternalResolution{UserID: user.ID, BindingID: binding.ID, Linked: true}
		return nil
	})
	if err != nil {
		return ExternalResolution{}, err
	}
	return result, nil
}

func (s ExternalIdentities) Finish(ctx context.Context, in ExternalCompletion, email RegistrationEmail) (ExternalFinish, error) {
	var out ExternalFinish
	err := s.Repository.WithinExternal(ctx, func(tx ExternalTx) error {
		if in.Evidence != nil {
			if !in.Evidence.valid() || in.Evidence.Authority != in.Authority || in.Evidence.Publisher != in.Publisher {
				return ErrUnavailable
			}
			user, binding, err := s.register(tx, *in.Evidence, email)
			if err != nil {
				return err
			}
			in.UserID = user.ID
			in.BindingID = binding.ID
		}
		user, err := tx.Account(in.UserID, nil)
		if err != nil {
			return err
		}
		binding, found, err := tx.FindBinding(in.BindingID)
		if err != nil {
			return err
		}
		if !found || binding.UserID != user.ID || binding.Authority != in.Authority || binding.Publisher != in.Publisher {
			return ErrUnavailable
		}
		if in.Linked {
			out.Linked = true
			return nil
		}
		if err := tx.TouchLogin(user.ID, s.Tokens.now().UTC()); err != nil {
			return err
		}
		token, expires, err := s.Tokens.Issue(SessionClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin})
		if err != nil {
			return err
		}
		if err := tx.Audit(user.PublicAccount, "plugin.identity.login", in.Authority, "plugin identity verified; core session issued"); err != nil {
			return err
		}
		out = ExternalFinish{User: user.PublicAccount, Token: token, ExpiresAt: expires}
		return nil
	})
	if err != nil {
		return ExternalFinish{}, err
	}
	return out, nil
}

func (s ExternalIdentities) RegistrationAvailable(ctx context.Context) error {
	return s.Repository.WithinExternal(ctx, func(tx ExternalTx) error {
		allowed, err := tx.RegistrationAllowed()
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRegistrationClosed
		}
		return nil
	})
}
