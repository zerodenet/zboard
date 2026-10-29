package entitlements

import (
	"context"
	"errors"
	"testing"
	"time"
)

type accessExpiryOnlyRepository struct {
	now    time.Time
	limit  int
	called bool
}

func (*accessExpiryOnlyRepository) ExpireDueCredentials(context.Context, time.Time, int) ([]ExpiredCredential, error) {
	panic("access pass called financial closure")
}
func (*accessExpiryOnlyRepository) HasDueCredentials(context.Context, time.Time) (bool, error) {
	return false, nil
}
func (s *accessExpiryOnlyRepository) ExpireDueAccessCredentials(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.now, s.limit, s.called = now, limit, true
	return 2, nil
}
func TestCredentialAccessExpiryUsesOnlyItsBoundedPort(t *testing.T) {
	if _, err := (CredentialExpiry{}).ExpireAccessDue(t.Context(), time.Now(), 1); !errors.Is(err, ErrCredentialExpiryUnavailable) {
		t.Fatal("missing access expiry port", err)
	}
	repo := &accessExpiryOnlyRepository{}
	at := time.Date(2026, 10, 29, 8, 0, 0, 0, time.FixedZone("fixture", 8*3600))
	if n, err := (CredentialExpiry{Repository: repo}).ExpireAccessDue(t.Context(), at, 0); err != nil || n != 2 || !repo.called || repo.limit != 200 || !repo.now.Equal(at) || repo.now.Location() != time.UTC {
		t.Fatal("bounded independent access port", n, err, repo)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if n, err := (CredentialExpiry{Repository: repo}).ExpireAccessDue(ctx, at, 1); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled access pass", n, err)
	}
}
