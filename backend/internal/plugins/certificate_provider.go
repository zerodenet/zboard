package plugins

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

const CertificateProviderCapability = "zboard.certificate.provider.v1"

type CertificateProviderView struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	PluginID string `json:"plugin_id"`
}

type ProviderDefinitionView struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	PluginID     string   `json:"plugin_id"`
	Capabilities []string `json:"capabilities"`
}

type CertificateIssueRequest struct {
	ProviderKey, Credential, ContactEmail, Environment, OperationID string
	CSRPEM                                                          []byte
	Domains                                                         []string
	Renewal                                                         bool
}

func (m *Manager) ProviderDefinitions(ctx context.Context) ([]ProviderDefinitionView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	owners, err := m.dnsProviderCatalog(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string][]ProviderDefinitionView{}
	for _, owner := range owners {
		owned := map[string]ProviderDefinitionView{}
		for _, declaration := range owner.declarations {
			owned[declaration.Key] = ProviderDefinitionView{Key: declaration.Key, Name: declaration.Name, PluginID: owner.snapshot.ID, Capabilities: []string{"dns.records"}}
		}
		for _, declaration := range owner.certificateDeclarations {
			definition := owned[declaration.Key]
			if definition.Key == "" {
				definition = ProviderDefinitionView{Key: declaration.Key, Name: declaration.Name, PluginID: owner.snapshot.ID}
			}
			if definition.Name != declaration.Name {
				continue
			}
			definition.Capabilities = append(definition.Capabilities, "certificate.issue")
			owned[declaration.Key] = definition
		}
		for key, definition := range owned {
			byKey[key] = append(byKey[key], definition)
		}
	}
	out := make([]ProviderDefinitionView, 0, len(byKey))
	for _, candidates := range byKey {
		if len(candidates) == 1 {
			out = append(out, candidates[0])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Manager) CertificateProviders(ctx context.Context) ([]CertificateProviderView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return nil, err
	}
	owners, err := m.dnsProviderCatalog(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string][]CertificateProviderView{}
	for _, owner := range owners {
		for _, declaration := range owner.certificateDeclarations {
			byKey[declaration.Key] = append(byKey[declaration.Key], CertificateProviderView{Key: declaration.Key, Name: declaration.Name, PluginID: owner.snapshot.ID})
		}
	}
	out := make([]CertificateProviderView, 0, len(byKey))
	for _, candidates := range byKey {
		if len(candidates) == 1 {
			out = append(out, candidates[0])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Manager) certificateProviderRuntime(ctx context.Context, key string) (Installation, *process, error) {
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
		for _, declaration := range owner.certificateDeclarations {
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

func (m *Manager) VerifyCertificateProviderCredential(ctx context.Context, key, credential string) error {
	if !providerKeyPattern.MatchString(key) || credential == "" || len(credential) > 4096 {
		return ErrPermission
	}
	m.mu.Lock()
	before, process, err := m.certificateProviderRuntime(ctx, key)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := process.api.VerifyCertificateCredential(callCtx, &pluginv1.CertificateCredentialRequest{
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
		return errors.New("certificate provider credential verification failed")
	}
	return nil
}

func (m *Manager) IssueCertificate(ctx context.Context, request CertificateIssueRequest) ([]byte, error) {
	if !providerKeyPattern.MatchString(request.ProviderKey) || request.Credential == "" || len(request.Credential) > 4096 || len(request.CSRPEM) == 0 || len(request.CSRPEM) > 32<<10 || len(request.Domains) == 0 || len(request.Domains) > 10 || len(request.ContactEmail) > 254 || (request.Environment != "production" && request.Environment != "staging") || strings.TrimSpace(request.OperationID) == "" || len(request.OperationID) > 128 {
		return nil, ErrPermission
	}
	m.mu.Lock()
	before, process, err := m.certificateProviderRuntime(ctx, request.ProviderKey)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	result, err := process.api.IssueCertificate(callCtx, &pluginv1.CertificateIssueRequest{
		ProviderKey: request.ProviderKey, Credential: []byte(request.Credential), CsrPem: request.CSRPEM,
		Domains: append([]string{}, request.Domains...), ContactEmail: request.ContactEmail,
		Environment: request.Environment, Renewal: request.Renewal, OperationId: request.OperationID,
		Generation: before.Generation, ConfigRevision: before.ConfigRevision,
	})
	if callCtx.Err() != nil {
		return nil, callCtx.Err()
	}
	m.mu.Lock()
	checkErr := m.checkRuntimeSnapshot(before)
	m.mu.Unlock()
	if checkErr != nil {
		return nil, checkErr
	}
	if err != nil || result == nil || len(result.FullchainPem) == 0 || len(result.FullchainPem) > 96<<10 {
		return nil, errors.New("certificate provider returned an invalid or failed result")
	}
	return append([]byte{}, result.FullchainPem...), nil
}
