package plugins

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/model"
	"time"
)

const MeteringReadCapability = "zboard.metering.read.v1"
const CommerceOrdersReadCapability = "zboard.commerce.orders.read.v1"

func pluginOperationCapability(operation string) string {
	switch operation {
	case "metering.usage.query":
		return MeteringReadCapability
	case "commerce.orders.list":
		return CommerceOrdersReadCapability
	default:
		return ""
	}
}

// SessionAuthority binds a transport-authenticated account to the plugin's
// admitted account page. It never grants administrator or target-user rights.
type SessionAuthority struct {
	Manager *Manager
	UserID  uint
	Admin   bool
}

type capabilityAdmissionWindow struct {
	StartedAt time.Time
	Count     int
}

func (a SessionAuthority) Resolve(ctx context.Context, c catalog.Credential, operation string) (catalog.Grant, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Grant{}, err
	}
	requiredCapability := pluginOperationCapability(operation)
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
	if session.UserID != a.UserID || session.Surface != "account" || session.Purpose != "business" {
		return catalog.Grant{}, catalog.ErrDenied
	}
	installation, err := m.load(session.PluginID)
	if err != nil {
		return catalog.Grant{}, ErrUnavailable
	}
	if !hasCapability(installation, requiredCapability) {
		return catalog.Grant{}, catalog.ErrDenied
	}
	var count int64
	if err := m.db.WithContext(ctx).Model(&model.User{}).Where("id = ? AND status = ?", a.UserID, "active").Count(&count).Error; err != nil {
		return catalog.Grant{}, err
	}
	if count != 1 {
		return catalog.Grant{}, catalog.ErrDenied
	}
	return catalog.Grant{Principal: catalog.Principal{
		Kind: "plugin_session", Subject: pluginCapabilitySubject(session), AccountID: a.UserID,
		PluginID: session.PluginID, Generation: session.Generation,
	}}, nil
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
	requiredCapability := pluginOperationCapability(descriptor.Name)
	if requiredCapability == "" || !hasCapability(installation, requiredCapability) {
		return catalog.ErrDenied
	}
	var activeAccounts int64
	if err := m.db.WithContext(ctx).Model(&model.User{}).Where("id = ? AND status = ?", principal.AccountID, "active").Count(&activeAccounts).Error; err != nil {
		return err
	}
	if activeAccounts != 1 {
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

func pluginCapabilitySubject(session Session) string {
	payload := fmt.Sprintf("%s:%d:%d", session.PluginID, session.Generation, session.UserID)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("plugin:%x", sum[:])
}
