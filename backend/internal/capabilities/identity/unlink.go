package identity

import (
	"context"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func ProviderOwnedBy(plugin, provider string) bool {
	return plugin != "" && (provider == plugin || strings.HasPrefix(provider, plugin+"~"))
}

type UnlinkRequest struct {
	ActorID, TargetID                     uint
	PluginID, BindingID                   string
	Administrative                        bool
	Password, Authorization, Confirmation string
}
type BindingRemovalRepository interface {
	RemoveBinding(context.Context, uint, uint, string, string, func(LoginAccount, LoginAccount, ExternalBinding) error) error
}
type BindingRemoval struct {
	Repository   BindingRemovalRepository
	Confirmation PasswordConfirmation
}

func (s BindingRemoval) Unlink(ctx context.Context, in UnlinkRequest) error {
	if in.ActorID == 0 || in.TargetID == 0 || in.PluginID == "" || in.BindingID == "" || len(in.BindingID) > 64 {
		return ErrPermission
	}
	if !in.Administrative && in.ActorID != in.TargetID {
		return ErrPermission
	}
	return s.Repository.RemoveBinding(ctx, in.ActorID, in.TargetID, in.PluginID, in.BindingID, func(actor, target LoginAccount, binding ExternalBinding) error {
		if actor.ID != in.ActorID || actor.Status != "active" || target.ID != in.TargetID || binding.UserID != target.ID || !ProviderOwnedBy(in.PluginID, binding.Authority) {
			return ErrPermission
		}
		if in.Administrative {
			if !actor.IsAdmin {
				return ErrPermission
			}
		} else {
			if target.Status != "active" {
				return ErrUnavailable
			}
			if !s.Confirmation.Valid(Account{ID: target.ID, PasswordHash: target.PasswordHash}, in.Authorization, in.Confirmation) && bcrypt.CompareHashAndPassword([]byte(target.PasswordHash), []byte(in.Password)) != nil {
				return ErrPassword
			}
		}
		return nil
	})
}
