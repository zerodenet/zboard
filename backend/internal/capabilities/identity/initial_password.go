package identity

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// InitialPasswordGrant is resolved by the adapter from a consumed recent-login
// grant. A user-supplied binding ID alone is never such a grant.
type InitialPasswordGrant struct {
	AccountID                       uint
	BindingID, Authority, Publisher string
}
type InitialPasswordRepository interface {
	SetInitialPassword(context.Context, Principal, InitialPasswordGrant, string) error
}
type InitialPasswords struct{ Repository InitialPasswordRepository }

func (s InitialPasswords) Setup(ctx context.Context, p Principal, grant InitialPasswordGrant, password string) error {
	if p.ID == 0 || p.ID != grant.AccountID || grant.BindingID == "" || grant.Authority == "" || grant.Publisher == "" {
		return ErrUnavailable
	}
	if !ValidPassword(password) {
		return ErrPasswordPolicy
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Repository.SetInitialPassword(ctx, p, grant, string(hash))
}
