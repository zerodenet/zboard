package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const DNSProviderCapability = "zboard.dns.provider.v1"

type DNSProviderView struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	PluginID string `json:"plugin_id"`
}

type dnsProviderCatalogRow struct {
	ID, State, Publisher, VersionID, Manifest, Capabilities, AuthorizedDigest string
	Enabled, NativeTrusted                                                    bool
	Generation, ConfigRevision                                                uint64
}

type dnsProviderOwner struct {
	snapshot                Installation
	process                 *process
	declarations            []DNSProviderDefinition
	certificateDeclarations []DNSProviderDefinition
}

// dnsProviderCatalog deliberately uses one bounded projection. Provider
// discovery and every execution avoid an installation/version/admission N+1.
func (m *Manager) dnsProviderCatalog(ctx context.Context) ([]dnsProviderOwner, error) {
	var rows []dnsProviderCatalogRow
	err := m.db.WithContext(ctx).Model(&model.PluginInstallation{}).
		Select("plugin_installations.id, plugin_installations.state, plugin_installations.enabled, plugin_installations.publisher, plugin_installations.generation, plugin_installations.config_revision, plugin_installations.version_id, plugin_versions.manifest, plugin_authorizations.capabilities, plugin_authorizations.digest AS authorized_digest, plugin_authorizations.native_trusted").
		Joins("JOIN plugin_versions ON plugin_versions.id = plugin_installations.version_id").
		Joins("LEFT JOIN plugin_authorizations ON plugin_authorizations.plugin_id = plugin_installations.id").
		Where("plugin_installations.enabled = ? AND plugin_installations.state = ?", true, "active").
		Order("plugin_installations.id").Limit(200).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]dnsProviderOwner, 0, len(rows))
	for _, row := range rows {
		var manifest Manifest
		var capabilities []string
		if json.Unmarshal([]byte(row.Manifest), &manifest) != nil || json.Unmarshal([]byte(row.Capabilities), &capabilities) != nil {
			return nil, errors.New("invalid admitted DNS provider metadata")
		}
		admitted := row.NativeTrusted && row.AuthorizedDigest == row.VersionID && slices.Equal(capabilities, manifest.Capabilities) && (slices.Contains(capabilities, DNSProviderCapability) || slices.Contains(capabilities, CertificateProviderCapability))
		process := m.processes[row.ID]
		if !admitted || row.ConfigRevision == 0 || process == nil || process.client.Exited() {
			continue
		}
		out = append(out, dnsProviderOwner{
			snapshot: Installation{PluginInstallation: model.PluginInstallation{
				ID: row.ID, State: row.State, Enabled: row.Enabled, Publisher: row.Publisher,
				Generation: row.Generation, ConfigRevision: row.ConfigRevision, VersionID: row.VersionID,
			}},
			process: process, declarations: manifest.Contributions.DNSProviders, certificateDeclarations: manifest.Contributions.CertificateProviders,
		})
	}
	return out, nil
}

// DNSProviders returns only providers backed by the currently active,
// configured runtime generation. Duplicate keys are deliberately omitted so
// an account can never be routed by installation order.
func (m *Manager) DNSProviders(ctx context.Context) ([]DNSProviderView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	owners, err := m.dnsProviderCatalog(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string][]DNSProviderView{}
	for _, owner := range owners {
		for _, declaration := range owner.declarations {
			byKey[declaration.Key] = append(byKey[declaration.Key], DNSProviderView{Key: declaration.Key, Name: declaration.Name, PluginID: owner.snapshot.ID})
		}
	}
	out := make([]DNSProviderView, 0, len(byKey))
	for _, candidates := range byKey {
		if len(candidates) == 1 {
			out = append(out, candidates[0])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Manager) dnsProviderRuntime(ctx context.Context, key string) (Installation, *process, error) {
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return Installation{}, nil, err
	}
	owners, err := m.dnsProviderCatalog(ctx)
	if err != nil {
		return Installation{}, nil, err
	}
	var selected Installation
	var selectedProcess *process
	for _, owner := range owners {
		for _, declaration := range owner.declarations {
			if declaration.Key != key {
				continue
			}
			if selected.ID != "" {
				return Installation{}, nil, ErrConflict
			}
			selected, selectedProcess = owner.snapshot, owner.process
		}
	}
	if selected.ID == "" {
		return Installation{}, nil, ErrUnavailable
	}
	return selected, selectedProcess, nil
}

func (m *Manager) VerifyDNSProviderCredential(ctx context.Context, key, credential string) error {
	if !providerKeyPattern.MatchString(key) || credential == "" || len(credential) > 4096 {
		return ErrPermission
	}
	m.mu.Lock()
	before, process, err := m.dnsProviderRuntime(ctx, key)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := process.api.VerifyDNSCredential(callCtx, &pluginv1.DNSCredentialRequest{
		ProviderKey: key, Credential: []byte(credential), Generation: before.Generation, ConfigRevision: before.ConfigRevision,
	})
	if callCtx.Err() != nil {
		return callCtx.Err()
	}
	m.mu.Lock()
	checkErr := m.checkRuntimeSnapshot(before)
	m.mu.Unlock()
	if checkErr != nil {
		return checkErr
	}
	if err != nil || result == nil || !result.Healthy {
		return errors.New("DNS provider credential verification failed")
	}
	return nil
}

func (m *Manager) ApplyDNSProvider(ctx context.Context, key, credential string, record network.ManagedDNSRecord, takeover bool) (network.ManagedDNSProviderResult, error) {
	if !providerKeyPattern.MatchString(key) || credential == "" || len(credential) > 4096 {
		return network.ManagedDNSProviderResult{}, ErrPermission
	}
	m.mu.Lock()
	before, process, err := m.dnsProviderRuntime(ctx, key)
	m.mu.Unlock()
	if err != nil {
		return network.ManagedDNSProviderResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	result, err := process.api.ApplyDNSRecord(callCtx, &pluginv1.DNSApplyRequest{
		ProviderKey: key, Credential: []byte(credential), RecordType: record.RecordType,
		Name: record.DomainName, Value: record.RecordValue, Ttl: int32(record.TTL), Proxied: record.Proxied,
		ProviderZoneId: record.ProviderZoneID, ProviderRecordId: record.ProviderRecordID, Takeover: takeover,
		Generation: before.Generation, ConfigRevision: before.ConfigRevision,
	})
	if callCtx.Err() != nil {
		return network.ManagedDNSProviderResult{}, callCtx.Err()
	}
	m.mu.Lock()
	checkErr := m.checkRuntimeSnapshot(before)
	m.mu.Unlock()
	if checkErr != nil {
		return network.ManagedDNSProviderResult{}, checkErr
	}
	if err != nil || !validDNSApplyResult(result, record) {
		return network.ManagedDNSProviderResult{}, errors.New("DNS provider returned an invalid or failed result")
	}
	return network.ManagedDNSProviderResult{
		ZoneID: result.ZoneId, RecordID: result.RecordId, RecordType: result.RecordType,
		Name: result.Name, Value: result.Value, TTL: int(result.Ttl), Proxied: result.Proxied,
	}, nil
}

func validDNSApplyResult(result *pluginv1.DNSApplyResult, expected network.ManagedDNSRecord) bool {
	if result == nil || result.ZoneId == "" || len(result.ZoneId) > 512 || result.RecordId == "" || len(result.RecordId) > 512 {
		return false
	}
	if result.RecordType != expected.RecordType || strings.ToLower(strings.TrimSuffix(result.Name, ".")) != expected.DomainName || result.Value != expected.RecordValue || net.ParseIP(result.Value) == nil {
		return false
	}
	return result.Ttl == 1 || result.Ttl >= 60 && result.Ttl <= 86400
}
