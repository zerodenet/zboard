package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type nativePluginAuthority struct {
	grant    catalog.Grant
	accounts identity.Accounts
}

func (a nativePluginAuthority) Resolve(context.Context, catalog.Credential, string) (catalog.Grant, error) {
	return a.grant, nil
}
func (a nativePluginAuthority) Admit(ctx context.Context, _ catalog.Credential, grant catalog.Grant, _ catalog.Descriptor) error {
	if grant.Principal.AccountID != a.grant.Principal.AccountID || grant.Principal.PluginID != a.grant.Principal.PluginID {
		return catalog.ErrDenied
	}
	account, err := a.accounts.Me(ctx, identity.Principal{ID: grant.Principal.AccountID})
	if err != nil {
		if errors.Is(err, identity.ErrUnavailable) {
			return catalog.ErrDenied
		}
		return catalog.ErrUnavailable
	}
	if grant.Administrative && !account.IsAdmin {
		return catalog.ErrDenied
	}
	return nil
}

func (s *PluginHostServices) callBusinessCapability(ctx context.Context, pluginID, capability, operation string, payload json.RawMessage) (json.RawMessage, *pluginv1.HostCallError) {
	if plugins.OperationCapability(operation) != capability {
		return nil, pluginHostError("forbidden")
	}
	var input map[string]json.RawMessage
	if plugins.DecodeStrict(payload, &input) != nil || input == nil {
		return nil, pluginHostError("invalid_request")
	}
	var token string
	if json.Unmarshal(input["principal_id"], &token) != nil || token == "" || len(token) > 256 {
		return nil, pluginHostError("invalid_request")
	}
	var userID uint
	var account identity.PublicAccount
	provider := ""
	if strings.HasPrefix(token, "p2.") {
		if capability != plugins.CommerceOrdersReadCapability && capability != plugins.CommercePaymentsWriteCapability {
			return nil, pluginHostError("forbidden")
		}
		scoped, err := s.checkoutPrincipal(ctx, pluginID, token)
		if err != nil {
			if errors.Is(err, plugins.ErrPermission) {
				return nil, pluginHostError("forbidden")
			}
			return nil, pluginHostError("temporary_unavailable")
		}
		userID = scoped.AccountID
		account = identity.PublicAccount{ID: userID}
		field := "id"
		if operation == "commerce.payments.record" {
			field = "order_id"
		}
		var selected uint
		if value, exists := input[field]; exists && json.Unmarshal(value, &selected) != nil {
			return nil, pluginHostError("invalid_request")
		}
		if selected != 0 && selected != scoped.OrderID {
			return nil, pluginHostError("forbidden")
		}
		input[field], _ = json.Marshal(scoped.OrderID)
		provider = scoped.Identity
	} else {
		if capability == plugins.CommercePaymentsWriteCapability {
			return nil, pluginHostError("forbidden")
		}
		var callErr *pluginv1.HostCallError
		userID, account, callErr = s.principal(ctx, pluginID, token)
		if callErr != nil {
			return nil, callErr
		}
	}
	admin := plugins.IsAdministrativeOperation(operation)
	if admin && !account.IsAdmin {
		return nil, pluginHostError("forbidden")
	}
	delete(input, "principal_id")
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, pluginHostError("invalid_request")
	}
	subject := sha256.Sum256([]byte(pluginID + "\x00" + token))
	grant := catalog.Grant{Principal: catalog.Principal{Kind: "native_plugin", PaymentProvider: provider, Subject: "plugin:" + hex.EncodeToString(subject[:]), AccountID: userID, PluginID: pluginID, Generation: 1}, Administrative: admin}
	authority := nativePluginAuthority{grant: grant, accounts: s.services.Identity.Accounts}
	registry := catalog.New(authority, authority)
	if err := s.services.RegisterPluginBusinessCapabilities(registry, s.projector); err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	if err := s.services.RegisterCommerceCapabilities(registry, s.cipher, s.mieru); err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	result, err := registry.Invoke(ctx, catalog.Credential{Kind: "native_plugin"}, operation, raw)
	if err != nil {
		switch {
		case errors.Is(err, catalog.ErrDenied):
			return nil, pluginHostError("forbidden")
		case errors.Is(err, catalog.ErrInput):
			return nil, pluginHostError("invalid_request")
		case errors.Is(err, catalog.ErrRateLimited):
			return nil, pluginHostError("rate_limited")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, pluginHostError("deadline_exceeded")
		case errors.Is(err, catalog.ErrUnknown):
			return nil, pluginHostError("capability_unavailable")
		case errors.Is(err, catalog.ErrConflict):
			return nil, pluginHostError("conflict")
		default:
			return nil, pluginHostError("temporary_unavailable")
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	return encoded, nil
}
