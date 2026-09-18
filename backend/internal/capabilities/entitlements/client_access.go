package entitlements

import (
	"context"
	"strings"
	"time"
)

// ClientGrant identifies the one subscription authorized by a presented token.
// It carries neither stored token ciphertext nor a reusable account credential.
type ClientGrant struct {
	Subscription Subscription
	AccessID     uint
	ObservedAt   time.Time
}
type ClientAccessRepository interface {
	Resolve(context.Context, string) (ClientGrant, error)
	MarkUsed(context.Context, string, uint, time.Time) error
}
type ClientAccess struct{ Repository ClientAccessRepository }

func (s ClientAccess) Resolve(ctx context.Context, raw string) (ClientGrant, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "/") {
		return ClientGrant{}, ErrAccessPermission
	}
	return s.Repository.Resolve(ctx, HashAccessToken(raw))
}
func (s ClientAccess) MarkUsed(ctx context.Context, raw string, id uint, at time.Time) error {
	if id == 0 || raw == "" {
		return ErrAccessPermission
	}
	return s.Repository.MarkUsed(ctx, HashAccessToken(strings.TrimSpace(raw)), id, at)
}
