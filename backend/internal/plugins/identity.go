package plugins

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
	"slices"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const IdentityCapability = "zboard.identity.provider.v1"

type IdentityProviderView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IdentitySnapshot binds every exchange and core commit to the same publisher,
// running generation and committed configuration used to begin authentication.
type IdentitySnapshot struct {
	ID         string
	Publisher  string
	Generation uint64
	Revision   uint64
	Provider   *pluginv1.IdentityProvider
}

func (m *Manager) IdentityProviders() ([]IdentityProviderView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []IdentityProviderView{}
	if err := m.guard(m.db); err != nil {
		return out, err
	}
	var rows []model.PluginInstallation
	if err := m.db.Where("enabled = ? AND state = ?", true, "active").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		v, err := m.load(row.ID)
		if err != nil {
			return nil, err
		}
		if slices.Contains(v.Manifest.Capabilities, IdentityCapability) && v.ConfigRevision > 0 && m.processes[v.ID] != nil {
			out = append(out, IdentityProviderView{ID: v.ID, Name: v.Name})
		}
	}
	return out, nil
}
func (m *Manager) identityProcess(id string) (Installation, *process, error) {
	if err := m.guard(m.db); err != nil {
		return Installation{}, nil, err
	}
	v, err := m.load(id)
	if err != nil {
		return v, nil, err
	}
	p := m.processes[id]
	if !v.Enabled || v.State != "active" || p == nil || p.client.Exited() || !slices.Contains(v.Manifest.Capabilities, IdentityCapability) {
		return v, nil, ErrUnavailable
	}
	return v, p, nil
}
func validIdentityProvider(p *pluginv1.IdentityProvider) bool {
	if p == nil || p.ClientId == "" || len(p.ClientId) > 512 || len(p.Scopes) > 16 || !slices.Contains(p.Scopes, "openid") {
		return false
	}
	for _, raw := range []string{p.Issuer, p.AuthorizationEndpoint} {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
			return false
		}
	}
	for _, scope := range p.Scopes {
		if len(scope) == 0 || len(scope) > 128 {
			return false
		}
		for _, r := range scope {
			if r < 0x21 || r > 0x7e || r == '"' || r == '\\' {
				return false
			}
		}
	}
	return true
}
func (m *Manager) IdentityProvider(ctx context.Context, id string) (IdentitySnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, p, err := m.identityProcess(id)
	if err != nil {
		return IdentitySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := p.api.GetIdentityProvider(ctx, &pluginv1.Empty{})
	if err != nil || !validIdentityProvider(info) {
		return IdentitySnapshot{}, errors.New("identity provider unavailable or invalid")
	}
	return IdentitySnapshot{ID: v.ID, Publisher: v.Publisher, Generation: v.Generation, Revision: v.ConfigRevision, Provider: info}, nil
}
func (m *Manager) checkIdentitySnapshot(snapshot IdentitySnapshot) (*process, error) {
	v, p, err := m.identityProcess(snapshot.ID)
	if err != nil {
		return nil, err
	}
	if v.Publisher != snapshot.Publisher || v.Generation != snapshot.Generation || v.ConfigRevision != snapshot.Revision {
		return nil, ErrConflict
	}
	return p, nil
}

// WithIdentityProvider serializes a host-owned commit with disable, uninstall,
// crash recovery and config changes. The plugin never receives commit or a DB.
func (m *Manager) WithIdentityProvider(snapshot IdentitySnapshot, commit func(*gorm.DB) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.checkIdentitySnapshot(snapshot); err != nil {
		return err
	}
	return m.identityTransaction(snapshot, commit)
}

// Hold lease and installation row locks in the SAME transaction as the core
// identity/session write. A takeover or configuration update cannot cross it.
func (m *Manager) identityTransaction(snapshot IdentitySnapshot, commit func(*gorm.DB) error) error {
	return m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		var row model.PluginInstallation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND enabled = ? AND state = ? AND generation = ? AND config_revision = ? AND publisher = ?", snapshot.ID, true, "active", snapshot.Generation, snapshot.Revision, snapshot.Publisher).First(&row).Error; err != nil {
			return ErrConflict
		}
		return commit(tx)
	})
}
func (m *Manager) ExchangeIdentity(ctx context.Context, snapshot IdentitySnapshot, request *pluginv1.IdentityExchange, commit func(*pluginv1.VerifiedIdentity, *gorm.DB) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.checkIdentitySnapshot(snapshot)
	if err != nil {
		return err
	}
	if snapshot.Provider == nil || request.Issuer != snapshot.Provider.Issuer {
		return ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	identity, err := p.api.ExchangeIdentity(ctx, request)
	if err != nil || identity == nil || identity.Issuer != request.Issuer || identity.Subject == "" || len(identity.Subject) > 512 {
		return errors.New("provider identity verification failed")
	}
	return m.identityTransaction(snapshot, func(tx *gorm.DB) error { return commit(identity, tx) })
}
