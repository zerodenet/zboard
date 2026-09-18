package identity

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUnavailable    = errors.New("account unavailable")
	ErrPassword       = errors.New("password verification failed")
	ErrPasswordPolicy = errors.New("password must contain 12 to 72 UTF-8 bytes")
	ErrConflict       = errors.New("account state changed")
)

// Principal is populated by an authenticated adapter. Self-service operations
// accept no separate target user ID and therefore cannot switch account scope.
type Principal struct {
	ID    uint
	Actor string
}
type Account struct {
	ID           uint
	PasswordHash string
}
type PasswordRepository interface {
	ActiveAccount(context.Context, uint) (Account, error)
	ReplacePasswordAndAudit(context.Context, Principal, Account, string) error
}
type PasswordService struct{ Repository PasswordRepository }

func (s PasswordService) Active(ctx context.Context, p Principal) (Account, error) {
	if p.ID == 0 {
		return Account{}, ErrUnavailable
	}
	account, err := s.Repository.ActiveAccount(ctx, p.ID)
	if err != nil {
		return Account{}, err
	}
	if account.ID != p.ID {
		return Account{}, ErrUnavailable
	}
	return account, nil
}

func ValidPassword(value string) bool { return len(value) >= 12 && len(value) <= 72 }

func (s PasswordService) Verify(ctx context.Context, p Principal, password string) (Account, error) {
	if p.ID == 0 {
		return Account{}, ErrUnavailable
	}
	account, err := s.Active(ctx, p)
	if err != nil {
		return Account{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return Account{}, ErrPassword
	}
	return account, nil
}

func (s PasswordService) Change(ctx context.Context, p Principal, current, password string) error {
	if !ValidPassword(password) {
		return ErrPasswordPolicy
	}
	before, err := s.Verify(ctx, p, current)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Repository.ReplacePasswordAndAudit(ctx, p, before, string(hash))
}

func (s PasswordService) HasLocalPassword(ctx context.Context, p Principal) (bool, error) {
	if p.ID == 0 {
		return false, ErrUnavailable
	}
	account, err := s.Active(ctx, p)
	if err != nil {
		return false, err
	}
	return account.PasswordHash != "!external", nil
}
