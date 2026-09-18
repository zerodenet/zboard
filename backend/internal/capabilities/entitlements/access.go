package entitlements

import (
	"context"
	"errors"
	"time"
)

var ErrAccessPermission = errors.New("subscription access requires current active owner")
var ErrAccessNotFound = errors.New("subscription not found")
var ErrAccessInactive = errors.New("subscription is inactive, expired, or out of traffic")

type AccessView struct {
	Configured      bool       `json:"configured"`
	SubscriptionID  uint       `json:"subscription_id"`
	Token           string     `json:"token,omitempty"`
	TokenPrefix     string     `json:"token_prefix,omitempty"`
	SubscriptionURL string     `json:"subscription_url,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	Revoked         bool       `json:"revoked,omitempty"`
	Notice          string     `json:"notice,omitempty"`
}

type AccessRepository interface {
	Execute(context.Context, uint, uint, string) (AccessView, error)
}
type Access struct{ Repository AccessRepository }

func (s Access) Read(ctx context.Context, actor, id uint) (AccessView, error) {
	return s.run(ctx, actor, id, "read")
}
func (s Access) Rotate(ctx context.Context, actor, id uint) (AccessView, error) {
	return s.run(ctx, actor, id, "rotate")
}
func (s Access) Revoke(ctx context.Context, actor, id uint) (AccessView, error) {
	return s.run(ctx, actor, id, "revoke")
}
func (s Access) run(ctx context.Context, actor, id uint, operation string) (AccessView, error) {
	if actor == 0 {
		return AccessView{}, ErrAccessPermission
	}
	if id == 0 {
		return AccessView{}, ErrAccessNotFound
	}
	return s.Repository.Execute(ctx, actor, id, operation)
}
func AccessAvailable(sub Subscription, now time.Time) bool {
	return sub.Status == "active" && sub.EndAt.After(now) && sub.FlowUsed < sub.FlowTotal
}
