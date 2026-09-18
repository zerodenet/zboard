package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrCredentials = errors.New("invalid email or password")

type PublicAccount struct {
	ID      uint   `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
	Status  string `json:"status"`
}
type LoginAccount struct {
	PublicAccount
	PasswordHash string `json:"-"`
}

// AccountRepository owns account reads and the conditional login commit. A
// successful password check must not revive a concurrently disabled account.
type AccountRepository interface {
	LoginAccount(context.Context, string) (LoginAccount, error)
	CompleteLogin(context.Context, LoginAccount, time.Time) (PublicAccount, error)
	CurrentAccount(context.Context, uint) (PublicAccount, error)
}
type Accounts struct {
	Repository AccountRepository
	Now        func() time.Time
}

func NormalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func (s Accounts) Login(ctx context.Context, email, password string) (PublicAccount, error) {
	email = NormalizeEmail(email)
	if email == "" || password == "" {
		return PublicAccount{}, ErrCredentials
	}
	account, err := s.Repository.LoginAccount(ctx, email)
	if errors.Is(err, ErrUnavailable) {
		return PublicAccount{}, ErrCredentials
	}
	if err != nil {
		return PublicAccount{}, err
	}
	if account.ID == 0 || account.Email != email || account.Status != "active" || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return PublicAccount{}, ErrCredentials
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	current, err := s.Repository.CompleteLogin(ctx, account, now)
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrConflict) {
		return PublicAccount{}, ErrCredentials
	}
	if err == nil && (current.ID != account.ID || current.Status != "active") {
		return PublicAccount{}, ErrCredentials
	}
	return current, err
}
func (s Accounts) Me(ctx context.Context, p Principal) (PublicAccount, error) {
	if p.ID == 0 {
		return PublicAccount{}, ErrUnavailable
	}
	account, err := s.Repository.CurrentAccount(ctx, p.ID)
	if err != nil {
		return PublicAccount{}, err
	}
	if account.ID != p.ID || account.Status != "active" {
		return PublicAccount{}, ErrUnavailable
	}
	return account, nil
}
