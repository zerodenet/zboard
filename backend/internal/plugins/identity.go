package plugins

import (
	"context"
	"errors"
	identitycap "github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/url"
	"regexp"
	"slices"
	"strings"
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

// IdentityFence is the persistence boundary required to atomically recheck a
// plugin runtime before committing host-owned identity state.
type IdentityFence struct {
	InstallationID string
	Publisher      string
	Generation     uint64
	Revision       uint64
	HostOwner      string
	HostEpoch      uint64
}

// IdentityServices contains only the core identity operations an admitted
// provider exchange may commit. It deliberately exposes neither GORM nor a
// generic database transaction to the plugin runtime or HTTP adapter.
type IdentityServices struct {
	External         identitycap.ExternalIdentities
	InitialPasswords identitycap.InitialPasswords
}

type IdentityTransactions interface {
	WithinIdentity(context.Context, IdentityFence, func(IdentityServices) error) error
}

func (m *Manager) SetIdentityTransactions(transactions IdentityTransactions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.identityTx = transactions
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
		if hasCapability(v, IdentityCapability) && v.ConfigRevision > 0 && m.processes[v.ID] != nil {
			p := m.processes[v.ID]
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			m.mu.Unlock()
			catalog, err := p.api.ListIdentityProviders(ctx, &pluginv1.Empty{})
			m.mu.Lock()
			cancel()
			if m.checkRuntimeSnapshot(v) != nil {
				continue
			}
			if status.Code(err) == codes.Unimplemented {
				out = append(out, IdentityProviderView{ID: v.ID, Name: v.Name})
				continue
			}
			if err != nil || catalog == nil || len(catalog.Providers) > 16 {
				continue
			}
			seen := map[string]bool{}
			for _, option := range catalog.Providers {
				if option == nil || !validProviderID(option.Id) || option.Name == "" || len(option.Name) > 100 || seen[option.Id] {
					continue
				}
				seen[option.Id] = true
				id := v.ID
				if option.Id != "" {
					id += "~" + option.Id
				}
				out = append(out, IdentityProviderView{ID: id, Name: option.Name})
			}
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
	if !v.Enabled || v.State != "active" || p == nil || p.client.Exited() || !hasCapability(v, IdentityCapability) {
		return v, nil, ErrUnavailable
	}
	return v, p, nil
}
func validIdentityProvider(p *pluginv1.IdentityProvider) bool {
	if p == nil || p.ClientId == "" || len(p.ClientId) > 512 || len(p.Scopes) > 16 {
		return false
	}
	if !validProviderID(p.ProviderId) || (p.Protocol != "" && p.Protocol != "oidc" && p.Protocol != "oauth2") {
		return false
	}
	if p.Protocol != "oauth2" && !slices.Contains(p.Scopes, "openid") {
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
	pluginID, providerID, _ := strings.Cut(id, "~")
	if !validProviderID(providerID) {
		return IdentitySnapshot{}, ErrUnavailable
	}
	v, p, err := m.identityProcess(pluginID)
	if err != nil {
		return IdentitySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	m.mu.Unlock()
	info, err := p.api.GetIdentityProvider(ctx, &pluginv1.IdentityProviderRequest{ProviderId: providerID})
	m.mu.Lock()
	if checkErr := m.checkRuntimeSnapshot(v); checkErr != nil {
		return IdentitySnapshot{}, checkErr
	}
	if err != nil || !validIdentityProvider(info) || info.ProviderId != providerID {
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
func (m *Manager) WithIdentityProvider(ctx context.Context, snapshot IdentitySnapshot, commit func(IdentityServices) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.checkIdentitySnapshot(snapshot); err != nil {
		return err
	}
	return m.identityTransaction(ctx, snapshot, commit)
}

// Hold lease and installation row locks in the SAME transaction as the core
// identity/session write. A takeover or configuration update cannot cross it.
func (m *Manager) identityTransaction(ctx context.Context, snapshot IdentitySnapshot, commit func(IdentityServices) error) error {
	if m.identityTx == nil || commit == nil {
		return ErrUnavailable
	}
	return m.identityTx.WithinIdentity(ctx, IdentityFence{
		InstallationID: snapshot.ID,
		Publisher:      snapshot.Publisher,
		Generation:     snapshot.Generation,
		Revision:       snapshot.Revision,
		HostOwner:      m.owner,
		HostEpoch:      m.epoch.Load(),
	}, commit)
}
func (m *Manager) ExchangeIdentity(ctx context.Context, snapshot IdentitySnapshot, request *pluginv1.IdentityExchange, commit func(*pluginv1.VerifiedIdentity, IdentityServices) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.checkIdentitySnapshot(snapshot)
	if err != nil {
		return err
	}
	if snapshot.Provider == nil || request.Issuer != snapshot.Provider.Issuer || request.ProviderId != snapshot.Provider.ProviderId {
		return ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	m.mu.Unlock()
	verified, err := p.api.ExchangeIdentity(ctx, request)
	m.mu.Lock()
	if _, checkErr := m.checkIdentitySnapshot(snapshot); checkErr != nil {
		return checkErr
	}
	if err != nil || verified == nil || verified.Issuer != request.Issuer || verified.Subject == "" || len(verified.Subject) > 512 || len(verified.Email) > 254 {
		return errors.New("provider identity verification failed")
	}
	return m.identityTransaction(ctx, snapshot, func(services IdentityServices) error { return commit(verified, services) })
}

var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func validProviderID(id string) bool { return id == "" || providerIDPattern.MatchString(id) }

// IdentityKey scopes bindings to a selected provider within the plugin.
func (s IdentitySnapshot) IdentityKey() string {
	if s.Provider != nil && s.Provider.ProviderId != "" {
		return s.ID + "~" + s.Provider.ProviderId
	}
	return s.ID
}
