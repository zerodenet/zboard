package plugins

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"time"
)

const MeteringReadCapability = "zboard.metering.read.v1"
const CommerceOrdersReadCapability = "zboard.commerce.orders.read.v1"

// OperationCapability is the single mapping from catalog operations to exact
// manifest grants for both page sessions and native host calls.
func OperationCapability(operation string) string {
	switch operation {
	case "metering.usage.query":
		return MeteringReadCapability
	case "commerce.orders.list":
		return CommerceOrdersReadCapability
	case "account.self.get":
		return AccountSelfReadCapability
	case "account.admin.get":
		return AccountAdminReadCapability
	case "subscriptions.owned.list", "subscriptions.owned.get":
		return SubscriptionReadCapability
	case "subscriptions.owned.config":
		return SubscriptionConfigReadCapability
	case "subscriptions.admin.list", "subscriptions.admin.get":
		return SubscriptionAdminReadCapability
	case "subscriptions.quota.adjust":
		return SubscriptionQuotaWriteCapability
	case "subscriptions.term.extend":
		return SubscriptionTermWriteCapability
	case "subscriptions.status.cancel":
		return SubscriptionStatusWriteCapability
	case "messages.owned.list", "messages.owned.get":
		return MessageReadCapability
	case "messages.owned.ack":
		return MessageAckCapability
	default:
		return ""
	}
}

// SessionAuthority binds a transport-authenticated account to an admitted
// business page or controlled slot. Administrative operations require a
// separate manifest grant, admin surface, and fresh account-domain role check.
type SessionAuthority struct {
	Manager  *Manager
	Accounts interface {
		Me(context.Context, identity.Principal) (identity.PublicAccount, error)
	}
	UserID uint
	Admin  bool
}

type capabilityAdmissionWindow struct {
	StartedAt time.Time
	Count     int
}

func (a SessionAuthority) Resolve(ctx context.Context, c catalog.Credential, operation string) (catalog.Grant, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Grant{}, err
	}
	requiredCapability := OperationCapability(operation)
	if a.Manager == nil || c.Kind != "plugin_session" || a.UserID == 0 || requiredCapability == "" {
		return catalog.Grant{}, catalog.ErrDenied
	}
	m := a.Manager
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.session(c.Proof)
	if errors.Is(err, ErrInvalidSession) {
		return catalog.Grant{}, catalog.ErrDenied
	}
	if err != nil {
		return catalog.Grant{}, err
	}
	if session.UserID != a.UserID || (session.Purpose != "business" && session.Purpose != "slot") || (session.Surface != "account" && session.Surface != "admin") {
		return catalog.Grant{}, catalog.ErrDenied
	}
	adminOperation := IsAdministrativeOperation(operation)
	if adminOperation && (session.Surface != "admin" || !a.Admin) {
		return catalog.Grant{}, catalog.ErrDenied
	}
	installation, err := m.load(session.PluginID)
	if err != nil {
		return catalog.Grant{}, ErrUnavailable
	}
	if !hasCapability(installation, requiredCapability) {
		return catalog.Grant{}, catalog.ErrDenied
	}
	if a.Accounts == nil {
		return catalog.Grant{}, catalog.ErrUnavailable
	}
	account, err := a.Accounts.Me(ctx, identity.Principal{ID: a.UserID})
	if err != nil || (adminOperation && !account.IsAdmin) {
		return catalog.Grant{}, catalog.ErrDenied
	}
	return catalog.Grant{Principal: catalog.Principal{
		Kind: "plugin_session", Subject: pluginCapabilitySubject(session), AccountID: a.UserID,
		PluginID: session.PluginID, Generation: session.Generation,
	}, Administrative: adminOperation}, nil
}

func (a SessionAuthority) Admit(ctx context.Context, credential catalog.Credential, grant catalog.Grant, descriptor catalog.Descriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	principal := grant.Principal
	if a.Manager == nil || credential.Kind != "plugin_session" || descriptor.RateLimitPerMinute < 1 || principal.Kind != "plugin_session" {
		return catalog.ErrUnavailable
	}
	m := a.Manager
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.session(credential.Proof)
	if errors.Is(err, ErrInvalidSession) {
		return catalog.ErrDenied
	}
	if err != nil {
		return err
	}
	if session.UserID != principal.AccountID || session.PluginID != principal.PluginID || session.Generation != principal.Generation || pluginCapabilitySubject(session) != principal.Subject {
		return catalog.ErrDenied
	}
	installation, err := m.load(session.PluginID)
	if err != nil {
		return ErrUnavailable
	}
	requiredCapability := OperationCapability(descriptor.Name)
	if requiredCapability == "" || !hasCapability(installation, requiredCapability) {
		return catalog.ErrDenied
	}
	if a.Accounts == nil {
		return catalog.ErrUnavailable
	}
	account, err := a.Accounts.Me(ctx, identity.Principal{ID: principal.AccountID})
	if err != nil || (IsAdministrativeOperation(descriptor.Name) && !account.IsAdmin) {
		return catalog.ErrDenied
	}
	now := time.Now().UTC()
	windowStart := now.Truncate(time.Minute)
	key := principal.Subject + "\x00" + descriptor.Name
	window := m.capabilityWindows[key]
	if !window.StartedAt.Equal(windowStart) {
		window = capabilityAdmissionWindow{StartedAt: windowStart}
	}
	if window.Count >= descriptor.RateLimitPerMinute {
		return catalog.ErrRateLimited
	}
	window.Count++
	m.capabilityWindows[key] = window
	if len(m.capabilityWindows) > 4096 {
		cutoff := windowStart.Add(-time.Minute)
		for candidate, value := range m.capabilityWindows {
			if value.StartedAt.Before(cutoff) {
				delete(m.capabilityWindows, candidate)
			}
		}
	}
	return nil
}

func IsAdministrativeOperation(operation string) bool {
	switch operation {
	case "account.admin.get", "subscriptions.admin.list", "subscriptions.admin.get", "subscriptions.quota.adjust", "subscriptions.term.extend", "subscriptions.status.cancel":
		return true
	default:
		return false
	}
}

func pluginCapabilitySubject(session Session) string {
	payload := fmt.Sprintf("%s:%d:%d", session.PluginID, session.Generation, session.UserID)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("plugin:%x", sum[:])
}
