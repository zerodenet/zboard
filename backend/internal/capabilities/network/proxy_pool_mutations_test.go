package network

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type proxyPoolMutationCipher struct{}

func (proxyPoolMutationCipher) Encrypt(value string) (string, error) { return "enc:" + value, nil }
func (proxyPoolMutationCipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

type proxyPoolMutationInspectorFunc func(context.Context, string, bool) (ProxyPoolConfigurationFacts, error)

func (f proxyPoolMutationInspectorFunc) InspectProxyPoolConfiguration(ctx context.Context, raw string, validateNative bool) (ProxyPoolConfigurationFacts, error) {
	return f(ctx, raw, validateNative)
}

type proxyPoolMutationRepositoryStub struct {
	snapshot ProxyPoolMutationSnapshot
	before   *ProxyPoolMutationSnapshot
	change   ProxyPoolMutationChange
}

func (s *proxyPoolMutationRepositoryStub) LoadProxyPoolMutation(_ context.Context, actor, id uint) (ProxyPoolMutationSnapshot, error) {
	if actor != 7 || id != s.snapshot.Pool.ID {
		return ProxyPoolMutationSnapshot{}, errors.New("unexpected load")
	}
	return s.snapshot, nil
}

func (s *proxyPoolMutationRepositoryStub) CommitProxyPoolMutation(_ context.Context, actor uint, before *ProxyPoolMutationSnapshot, change ProxyPoolMutationChange) (ProxyPoolRecord, error) {
	if actor != 7 {
		return ProxyPoolRecord{}, errors.New("unexpected actor")
	}
	s.before, s.change = before, change
	return ProxyPoolRecord{ID: 3, NodeID: change.NodeID, Name: change.Name, Revision: 5}, nil
}

func TestProxyPoolMutationsPrepareEncryptedVersionedChange(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	repository := &proxyPoolMutationRepositoryStub{snapshot: ProxyPoolMutationSnapshot{
		Pool:             ProxyPoolRecord{ID: 3, NodeID: 9, Name: "before", Revision: 4, AutoSync: true},
		ConfigCiphertext: "enc:old-config", SubscriptionURLCiphertext: "enc:https://old.example/sub",
	}}
	service := ProxyPoolMutations{
		Repository: repository, Cipher: proxyPoolMutationCipher{}, Now: func() time.Time { return now },
		Inspector: proxyPoolMutationInspectorFunc(func(_ context.Context, raw string, validateNative bool) (ProxyPoolConfigurationFacts, error) {
			if raw != "new-config" || !validateNative {
				t.Fatalf("config=%q validate_native=%v", raw, validateNative)
			}
			return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
	config, subscriptionURL, auto := "new-config", "https://new.example/sub", false
	got, err := service.Save(context.Background(), 7, ProxyPoolMutationRequest{
		ID: 3, NodeID: 9, Name: " next ", ExpectedRevision: 4, Config: &config,
		SubscriptionURL: &subscriptionURL, SubscriptionFormat: "zero", AutoSync: &auto, SyncIntervalSeconds: 600,
	})
	if err != nil || got.ID != 3 {
		t.Fatalf("result=%+v error=%v", got, err)
	}
	if repository.before == nil || repository.change.ConfigCiphertext != "enc:new-config" || repository.change.SubscriptionURLCiphertext != "enc:https://new.example/sub" || !repository.change.SubscriptionSourceChanged || repository.change.Name != "next" || !repository.change.Now.Equal(now) {
		t.Fatalf("change=%+v before=%+v", repository.change, repository.before)
	}
}

func TestProxyPoolMutationsRejectStaleAndInvalidInputs(t *testing.T) {
	repository := &proxyPoolMutationRepositoryStub{snapshot: ProxyPoolMutationSnapshot{Pool: ProxyPoolRecord{ID: 3, NodeID: 9, Revision: 4}, ConfigCiphertext: "enc:config"}}
	service := ProxyPoolMutations{Repository: repository, Cipher: proxyPoolMutationCipher{}, Inspector: proxyPoolMutationInspectorFunc(func(context.Context, string, bool) (ProxyPoolConfigurationFacts, error) {
		return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
	})}
	if _, err := service.Save(context.Background(), 7, ProxyPoolMutationRequest{ID: 3, NodeID: 9, Name: "pool", ExpectedRevision: 3}); !errors.Is(err, ErrProxyPoolConflict) {
		t.Fatalf("stale error=%v", err)
	}
	if _, err := service.Save(context.Background(), 7, ProxyPoolMutationRequest{NodeID: 9, Name: "pool"}); err == nil {
		t.Fatal("create without configuration accepted")
	}
}

func TestProxyPoolMutationsInspectUnchangedConfigurationWithoutNativeValidation(t *testing.T) {
	repository := &proxyPoolMutationRepositoryStub{snapshot: ProxyPoolMutationSnapshot{
		Pool: ProxyPoolRecord{ID: 3, NodeID: 9, Revision: 4}, ConfigCiphertext: "enc:existing-config",
	}}
	service := ProxyPoolMutations{
		Repository: repository, Cipher: proxyPoolMutationCipher{},
		Inspector: proxyPoolMutationInspectorFunc(func(_ context.Context, raw string, validateNative bool) (ProxyPoolConfigurationFacts, error) {
			if raw != "existing-config" || validateNative {
				t.Fatalf("config=%q validate_native=%v", raw, validateNative)
			}
			return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
	if _, err := service.Save(context.Background(), 7, ProxyPoolMutationRequest{ID: 3, NodeID: 9, Name: "renamed", ExpectedRevision: 4}); err != nil {
		t.Fatal(err)
	}
	if repository.change.ReplaceConfig {
		t.Fatal("unchanged configuration was replaced")
	}
}
