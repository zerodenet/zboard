package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const CodeLifetime = 10 * time.Minute
const CodeCooldown = time.Minute
const CodeSourceAddressLimit = 20

var (
	ErrCodeDisabled    = errors.New("registration verification disabled")
	ErrCodeCooldown    = errors.New("registration verification cooldown")
	ErrCodeRateLimited = errors.New("registration verification source limit")
	ErrCodeDelivery    = errors.New("registration verification delivery failed")
)

// External is supplied by the trusted external-registration adapter, never a
// request field. SourceHash is derived from the transport peer by that adapter.
type CodeRequest struct {
	Email, SourceHash string
	External          bool
}
type IssuedCode struct {
	Email, Hash, SourceHash string
	SentAt, ExpiresAt       time.Time
}
type CodeIssuanceTx interface {
	RegistrationAllowed() (bool, error)
	VerificationRequired() (bool, error)
	EmailInUse(string) (bool, error)
	RecentSourceAddresses(string, time.Time) (int64, error)
	LastCodeSent(string) (time.Time, bool, error)
	StoreCode(IssuedCode) error
}
type CodeIssuanceRepository interface {
	WithinCodeIssuance(context.Context, func(CodeIssuanceTx) error) error
	InvalidateCode(context.Context, IssuedCode, time.Time) error
}
type CodeDelivery interface {
	SendRegistrationCode(context.Context, string, string) error
}
type CodeIssuance struct {
	Repository CodeIssuanceRepository
	Codes      EmailCodes
	Delivery   CodeDelivery
	Now        func() time.Time
}

func GenerateEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
func (s CodeIssuance) Send(ctx context.Context, in CodeRequest) error {
	in.Email = NormalizeEmail(in.Email)
	if !ValidEmail(in.Email) {
		return &AccountValidation{Fields: map[string]string{"email": "请输入有效邮箱。"}}
	}
	if len(s.Codes.Key) == 0 || in.SourceHash == "" {
		return ErrUnavailable
	}
	code, err := GenerateEmailCode()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if s.Now != nil {
		now = s.Now().UTC().Truncate(time.Millisecond)
	}
	issued := IssuedCode{Email: in.Email, Hash: s.Codes.Digest(in.Email, code), SourceHash: in.SourceHash, SentAt: now, ExpiresAt: now.Add(CodeLifetime)}
	err = s.Repository.WithinCodeIssuance(ctx, func(tx CodeIssuanceTx) error {
		allowed, err := tx.RegistrationAllowed()
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRegistrationClosed
		}
		enabled, err := tx.VerificationRequired()
		if err != nil {
			return err
		}
		if !enabled && !in.External {
			return ErrCodeDisabled
		}
		exists, err := tx.EmailInUse(in.Email)
		if err != nil {
			return err
		}
		if exists {
			return ErrEmailConflict
		}
		count, err := tx.RecentSourceAddresses(in.SourceHash, now.Add(-time.Hour))
		if err != nil {
			return err
		}
		if count >= CodeSourceAddressLimit {
			return ErrCodeRateLimited
		}
		last, found, err := tx.LastCodeSent(in.Email)
		if err != nil {
			return err
		}
		if found && last.Add(CodeCooldown).After(now) {
			return ErrCodeCooldown
		}
		return tx.StoreCode(issued)
	})
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, 25*time.Second)
	err = s.Delivery.SendRegistrationCode(call, in.Email, code)
	cancel()
	if err == nil {
		return nil
	}
	// A disconnected requester must not prevent invalidation. Match this exact
	// issuance so a slow failed delivery cannot revoke a newer challenge.
	cleanup, release := context.WithTimeout(context.Background(), 5*time.Second)
	defer release()
	return errors.Join(ErrCodeDelivery, s.Repository.InvalidateCode(cleanup, issued, now))
}
