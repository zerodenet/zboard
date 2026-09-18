package network

import (
	"context"
	"errors"
	"testing"
)

type networkEntryMutationRepositoryStub struct {
	snapshot NetworkEntryMutationSnapshot
	pool     NetworkEntryProxyPoolSnapshot
	before   *NetworkEntryMutationSnapshot
	change   NetworkEntryMutationChange
}

func (s *networkEntryMutationRepositoryStub) LoadNetworkEntryMutation(_ context.Context, actor, id uint) (NetworkEntryMutationSnapshot, error) {
	if actor != 7 || id != s.snapshot.Entry.ID {
		return NetworkEntryMutationSnapshot{}, ErrNetworkEntryNotFound
	}
	return s.snapshot, nil
}

func (s *networkEntryMutationRepositoryStub) LoadNetworkEntryProxyPool(_ context.Context, actor, id uint) (NetworkEntryProxyPoolSnapshot, error) {
	if actor != 7 || id != s.pool.ID {
		return NetworkEntryProxyPoolSnapshot{}, errors.New("unexpected pool")
	}
	return s.pool, nil
}

func (s *networkEntryMutationRepositoryStub) CommitNetworkEntryMutation(_ context.Context, actor uint, before *NetworkEntryMutationSnapshot, change NetworkEntryMutationChange) (NetworkEntryMutationResult, error) {
	if actor != 7 {
		return NetworkEntryMutationResult{}, ErrNetworkEntryPermission
	}
	s.before, s.change = before, change
	change.Entry.ID, change.Entry.Revision = 5, 1
	return NetworkEntryMutationResult{Entry: change.Entry, HasPath: change.PathCiphertext != ""}, nil
}

func TestNetworkEntryMutationsPrepareEncryptedAtomicChange(t *testing.T) {
	repository := &networkEntryMutationRepositoryStub{}
	service := NetworkEntryMutations{
		Repository: repository, Cipher: proxyPoolMutationCipher{},
		Inspector: proxyPoolMutationInspectorFunc(func(_ context.Context, raw string, validateNative bool) (ProxyPoolConfigurationFacts, error) {
			if raw != "path" || validateNative {
				t.Fatalf("raw=%q validate_native=%v", raw, validateNative)
			}
			return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
	result, err := service.Save(context.Background(), 7, NetworkEntryMutationRequest{
		NodeID: 1, EndpointID: 2, Name: " front ", Network: "tcp_udp", Address: " entry.example.test ",
		Port: 443, Enabled: true, ReplacePath: true, Path: "path",
		MembershipChanges:   []NetworkEntryMembershipChange{{NodeGroupID: 9, ExpectedRevision: 1, Member: true}, {NodeGroupID: 3, ExpectedRevision: 2, Member: true}},
		CredentialProtocols: []string{"VLESS", "vless"},
	})
	if err != nil || result.Entry.ID != 5 || !result.HasPath {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if repository.change.PathCiphertext != "enc:path" || repository.change.Entry.Name != "front" || repository.change.Entry.Address != "entry.example.test" || repository.change.Entry.PublicPort != 443 {
		t.Fatalf("change=%+v", repository.change)
	}
	if repository.change.MembershipChanges[0].NodeGroupID != 3 || len(repository.change.CredentialProtocols) != 1 || repository.change.CredentialProtocols[0] != "vless" {
		t.Fatalf("normalized change=%+v", repository.change)
	}
}

func TestNetworkEntryMutationsRejectStaleAndRetainedTCPOnlyPathForUDP(t *testing.T) {
	repository := &networkEntryMutationRepositoryStub{snapshot: NetworkEntryMutationSnapshot{
		Entry: NetworkEntryRecord{ID: 5, NodeID: 1, EndpointID: 2, Revision: 4}, PathCiphertext: "enc:tcp-only",
	}}
	service := NetworkEntryMutations{
		Repository: repository, Cipher: proxyPoolMutationCipher{},
		Inspector: proxyPoolMutationInspectorFunc(func(_ context.Context, raw string, _ bool) (ProxyPoolConfigurationFacts, error) {
			if raw == "tcp-only" {
				return ProxyPoolConfigurationFacts{DatagramError: "HTTP CONNECT does not support UDP"}, nil
			}
			return ProxyPoolConfigurationFacts{SupportsDatagram: true}, nil
		}),
	}
	if err := service.CheckRevision(context.Background(), 7, 5, 3); !errors.Is(err, ErrNetworkEntryConflict) {
		t.Fatalf("revision error=%v", err)
	}
	_, err := service.Save(context.Background(), 7, NetworkEntryMutationRequest{
		ID: 5, ExpectedRevision: 4, NodeID: 1, EndpointID: 2, Name: "front", Network: "tcp_udp",
		Address: "entry.example.test", Port: 443, PublicPort: 443, Enabled: true,
	})
	var validation *NetworkEntryMutationValidation
	if !errors.As(err, &validation) {
		t.Fatalf("retained incompatible path error=%v", err)
	}
}
