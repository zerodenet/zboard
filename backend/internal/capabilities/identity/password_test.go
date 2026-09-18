package identity

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type passwordRepository struct {
	account Account
	swapped bool
	err     error
}

func (r *passwordRepository) ActiveAccount(context.Context, uint) (Account, error) {
	return r.account, nil
}
func (r *passwordRepository) ReplacePasswordAndAudit(_ context.Context, p Principal, a Account, hash string) error {
	if r.err != nil {
		return r.err
	}
	r.swapped = true
	r.account.PasswordHash = hash
	return nil
}

func TestPasswordCapabilityVerifiesScopeAndCurrentCredentialBeforeWriting(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password-123"), bcrypt.MinCost)
	r := &passwordRepository{account: Account{ID: 7, PasswordHash: string(hash)}}
	s := PasswordService{Repository: r}
	ctx := context.Background()
	if err := s.Change(ctx, Principal{ID: 8}, "old-password-123", "new-password-123"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := s.Change(ctx, Principal{ID: 7}, "wrong", "new-password-123"); !errors.Is(err, ErrPassword) {
		t.Fatal(err)
	}
	if r.swapped {
		t.Fatal("unverified write")
	}
	r.err = ErrConflict
	if err := s.Change(ctx, Principal{ID: 7}, "old-password-123", "new-password-123"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	r.err = nil
	if err := s.Change(ctx, Principal{ID: 7}, "old-password-123", "new-password-123"); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(r.account.PasswordHash), []byte("new-password-123")) != nil {
		t.Fatal("password was not replaced")
	}
}
