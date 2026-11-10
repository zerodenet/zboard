package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CompositionSlot struct {
	ConfigRevision uint64 `json:"config_revision"`
	CatalogSlot
	Availability   string                 `json:"availability"`
	Presentation   string                 `json:"presentation"`
	ReservedHeight int                    `json:"reserved_height"`
	Providers      []IdentityProviderView `json:"providers,omitempty"`
}
type UIComposition struct {
	Revision string            `json:"revision"`
	Slots    []CompositionSlot `json:"slots"`
}
type identityPresentationEntry struct {
	key       string
	providers []IdentityProviderView
	available bool
	expires   time.Time
}

func slotHeight(name string) int {
	switch name {
	case "account.overview.cards", "account.security.identities", "admin.user.identities":
		return 224
	case CheckoutPaymentSlot, "account.orders.payment":
		return 0 // Host-rendered checkout owns presentation.
	default:
		return 120
	}
}

// Presentation is a bounded projection of admitted declarations, never a session
// or an authorization grant. Enabled but temporarily unavailable entries remain.
func (m *Manager) UIComposition(ctx context.Context, surface string, userID uint, admin bool) (UIComposition, error) {
	out := UIComposition{Slots: []CompositionSlot{}}
	if surface != "public" && surface != "account" && surface != "admin" {
		return out, ErrPermission
	}
	if surface == "admin" && !admin || surface == "account" && userID == 0 {
		return out, ErrPermission
	}
	rows, err := m.List()
	if err != nil {
		return out, err
	}
	allowed := map[string]bool{}
	for _, v := range rows {
		if v.Enabled {
			allowed[v.ID] = true
		}
	}
	m.mu.Lock()
	for id := range m.identityPresentation {
		if !allowed[id] {
			delete(m.identityPresentation, id)
		}
	}
	m.mu.Unlock()
	type providerResult struct {
		views     []IdentityProviderView
		available bool
		key       string
	}
	providers := map[string]providerResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	// All provider metadata shares one deadline; slow runtimes cannot serialize
	// the page bootstrap. Only public names/IDs are cached for the same revision.
	metadataCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	limit := make(chan struct{}, 8)
	for _, v := range rows {
		if surface != "public" || !v.Enabled || !slotCapabilityAllowed(v, "auth.login.methods") {
			continue
		}
		declared := false
		for _, slot := range v.Manifest.Contributions.Slots {
			if slot.Surface == surface && slot.Slot == "auth.login.methods" {
				declared = true
			}
		}
		if !declared {
			continue
		}
		wg.Add(1)
		go func(v Installation) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-metadataCtx.Done():
				return
			}
			views, available := m.presentationProviders(metadataCtx, v)
			mu.Lock()
			providers[v.ID] = providerResult{views, available, fmt.Sprintf("%d:%d:%s", v.Generation, v.ConfigRevision, v.VersionID)}
			mu.Unlock()
		}(v)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	rows, err = m.List()
	if err != nil {
		return out, err
	}
	for _, v := range rows {
		if !v.Enabled {
			continue
		}
		for _, slot := range v.Manifest.Contributions.Slots {
			if slot.Surface != surface || !slotCapabilityAllowed(v, slot.Slot) {
				continue
			}
			item := CompositionSlot{ConfigRevision: v.ConfigRevision, CatalogSlot: CatalogSlot{PluginID: v.ID, Slot: slot, Generation: v.Generation}, Availability: "ready", Presentation: "frame", ReservedHeight: slotHeight(slot.Slot)}
			if m.lost.Load() || v.State != "active" {
				item.Availability = "unavailable"
			}
			if slot.Slot == "auth.login.methods" {
				item.Presentation = "identity"
				item.ReservedHeight = 36
				r := providers[v.ID]
				if r.key == fmt.Sprintf("%d:%d:%s", v.Generation, v.ConfigRevision, v.VersionID) {
					item.Providers = r.views
				} else {
					r.available = false
				}
				if !r.available {
					item.Availability = "unavailable"
				}
			}
			out.Slots = append(out.Slots, item)
		}
	}
	sort.SliceStable(out.Slots, func(i, j int) bool {
		a, b := out.Slots[i], out.Slots[j]
		return a.PluginID+":"+a.Slot.ID < b.PluginID+":"+b.Slot.ID
	})
	raw, _ := json.Marshal(out.Slots)
	sum := sha256.Sum256(raw)
	out.Revision = hex.EncodeToString(sum[:])
	return out, nil
}
func (m *Manager) presentationProviders(ctx context.Context, v Installation) ([]IdentityProviderView, bool) {
	key := fmt.Sprintf("%s:%d:%d:%s", v.Publisher, v.Generation, v.ConfigRevision, v.VersionID)
	m.mu.Lock()
	cached := m.identityPresentation[v.ID]
	if cached.key != key {
		cached = identityPresentationEntry{key: key}
	}
	p := m.processes[v.ID]
	if m.guard(m.db) != nil || v.State != "active" || v.ConfigRevision == 0 || p == nil || p.client.Exited() {
		m.mu.Unlock()
		return cached.providers, false
	}
	if time.Now().Before(cached.expires) {
		m.mu.Unlock()
		return cached.providers, cached.available
	}

	flightKey := v.ID + ":" + key
	if flight := m.identityPresentationLoading[flightKey]; flight != nil {
		m.mu.Unlock()
		select {
		case <-flight:
			return m.presentationProviders(ctx, v)
		case <-ctx.Done():
			return cached.providers, false
		}
	}
	if m.identityPresentationLoading == nil {
		m.identityPresentationLoading = map[string]chan struct{}{}
	}
	flight := make(chan struct{})
	m.identityPresentationLoading[flightKey] = flight
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.identityPresentationLoading, flightKey); close(flight); m.mu.Unlock() }()
	catalog, err := p.api.ListIdentityProviders(ctx, &pluginv1.Empty{})
	views := []IdentityProviderView{}
	available := err == nil && catalog != nil && len(catalog.Providers) <= 16
	if status.Code(err) == codes.Unimplemented {
		views = append(views, IdentityProviderView{ID: v.ID, Name: v.Name})
		available = true
	}
	if available && catalog != nil {
		seen := map[string]bool{}
		for _, option := range catalog.Providers {
			if option == nil || !validProviderID(option.Id) || strings.TrimSpace(option.Name) == "" || len(option.Name) > 100 || seen[option.Id] {
				continue
			}
			seen[option.Id] = true
			id := v.ID
			if option.Id != "" {
				id += "~" + option.Id
			}
			views = append(views, IdentityProviderView{ID: id, Name: option.Name})
		}
	}
	available = available && len(views) > 0
	if available {
		sort.SliceStable(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	}
	if !available {
		views = cached.providers
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checkRuntimeSnapshot(v) != nil || m.processes[v.ID] != p {
		return nil, false
	}
	if m.identityPresentation == nil {
		m.identityPresentation = map[string]identityPresentationEntry{}
	}
	ttl := 30 * time.Second
	if !available {
		ttl = 5 * time.Second
	}
	m.identityPresentation[v.ID] = identityPresentationEntry{key: key, providers: views, available: available, expires: time.Now().Add(ttl)}
	return views, available
}

// IdentityProviderViews reads only one admitted plugin's display metadata.
// Private identity widgets need not enumerate or wait on unrelated providers.
func (m *Manager) IdentityProviderViews(ctx context.Context, id string) ([]IdentityProviderView, error) {
	m.mu.Lock()
	v, err := m.load(id)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !v.Enabled || !hasCapability(v, IdentityCapability) {
		return nil, ErrPermission
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	views, available := m.presentationProviders(ctx, v)
	if !available {
		return nil, ErrUnavailable
	}
	return views, nil
}
