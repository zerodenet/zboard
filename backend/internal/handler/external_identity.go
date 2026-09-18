package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func externalEvidence(provider plugins.IdentitySnapshot, proof *pluginv1.VerifiedIdentity) identity.ExternalEvidence {
	return identity.ExternalEvidence{Authority: provider.IdentityKey(), Publisher: provider.Publisher, Issuer: proof.Issuer, Subject: proof.Subject, Email: proof.Email, EmailVerified: proof.EmailVerified}
}
func externalIdentityID(provider plugins.IdentitySnapshot, proof *pluginv1.VerifiedIdentity) string {
	return externalEvidence(provider, proof).Key()
}
func (h *handlers) resolveExternalIdentity(ctx context.Context, services plugins.IdentityServices, flow externalAuthFlow, proof *pluginv1.VerifiedIdentity, result *externalAuthCompletion) error {
	resolved, err := services.External.Resolve(ctx, externalEvidence(flow.Provider, proof), identity.BindingConfirmation{AccountID: flow.BindUserID, PasswordHash: flow.PasswordHash})
	if err != nil {
		return err
	}
	result.UserID = resolved.UserID
	result.IdentityID = resolved.BindingID
	result.Linked = resolved.Linked
	if resolved.RegistrationRequired {
		result.Identity = proof
	}
	return nil
}
func (h *handlers) finishExternalIdentity(ctx context.Context, services plugins.IdentityServices, result externalAuthCompletion) (any, error) {
	return h.finishExternalIdentityRegistration(ctx, services, result, externalRegistrationInput{})
}
func (h *handlers) finishExternalIdentityRegistration(ctx context.Context, services plugins.IdentityServices, result externalAuthCompletion, input externalRegistrationInput) (any, error) {
	in := identity.ExternalCompletion{Authority: result.Provider.IdentityKey(), Publisher: result.Provider.Publisher, UserID: result.UserID, BindingID: result.IdentityID, Linked: result.Linked}
	if result.Identity != nil {
		evidence := externalEvidence(result.Provider, result.Identity)
		in.Evidence = &evidence
	}
	finished, err := services.External.Finish(ctx, in, identity.RegistrationEmail{Email: input.Email, Code: input.VerificationCode})
	if err != nil {
		return nil, err
	}
	if finished.Linked {
		return map[string]bool{"linked": true}, nil
	}
	return map[string]any{"user": userPublic{ID: finished.User.ID, Email: finished.User.Email, IsAdmin: finished.User.IsAdmin, Status: finished.User.Status}, "auth": tokenResponse{Token: finished.Token, ExpiresAt: finished.ExpiresAt}}, nil
}
