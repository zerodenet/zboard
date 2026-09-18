package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

type pluginIdentityBinding struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Publisher  string    `json:"publisher"`
	Issuer     string    `json:"issuer"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"created_at"`
}

func pluginOwnsProvider(pluginID, providerID string) bool {
	return identity.ProviderOwnedBy(pluginID, providerID)
}

func (h *handlers) pluginIdentityProviderViews(pluginID string) ([]plugins.IdentityProviderView, error) {
	if h.identityProviders == nil {
		return nil, plugins.ErrUnavailable
	}
	providers, err := h.identityProviders.IdentityProviders()
	if err != nil {
		return nil, err
	}
	out := make([]plugins.IdentityProviderView, 0, len(providers))
	for _, provider := range providers {
		if pluginOwnsProvider(pluginID, provider.ID) {
			out = append(out, provider)
		}
	}
	return out, nil
}

func (h *handlers) pluginIdentityBindings(userID uint, pluginID string) ([]pluginIdentityBinding, error) {
	rows, err := h.services.Identity.Relationships.Bindings(context.Background(), userID)
	if err != nil {
		return nil, err
	}
	out := []pluginIdentityBinding{}
	for _, row := range rows {
		if pluginOwnsProvider(pluginID, row.Authority) {
			out = append(out, pluginIdentityBinding{ID: row.ID, ProviderID: row.Authority, Publisher: row.Publisher, Issuer: row.Issuer, Subject: row.Subject, CreatedAt: row.CreatedAt})
		}
	}
	return out, nil
}

func (h *handlers) unlinkPluginIdentity(session plugins.Session, claims authClaims, identityID, password string) error {
	return h.unlinkPluginIdentityConfirmed(session, claims, identityID, password, nil)
}
func (h *handlers) unlinkPluginIdentityConfirmed(session plugins.Session, claims authClaims, identityID, password string, r *http.Request) error {
	if (session.Surface != "account" && session.Surface != "admin") || session.UserID != claims.UserID {
		return plugins.ErrPermission
	}
	in := identity.UnlinkRequest{ActorID: claims.UserID, TargetID: session.TargetUserID, PluginID: session.PluginID, BindingID: identityID, Administrative: session.Surface == "admin", Password: password}
	if r != nil {
		in.Authorization = r.Header.Get("Authorization")
		in.Confirmation = r.Header.Get("X-ZBoard-Account-Confirmation")
	}
	service := h.services.Identity.Bindings
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	err := service.Unlink(ctx, in)
	if errors.Is(err, identity.ErrPermission) {
		return plugins.ErrPermission
	}
	if errors.Is(err, identity.ErrPassword) {
		return errAccountPasswordConfirmation
	}
	return err
}
