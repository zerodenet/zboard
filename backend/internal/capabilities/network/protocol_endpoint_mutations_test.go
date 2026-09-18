package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type protocolEndpointMutationRepositoryStub struct {
	snapshot ProtocolEndpointMutationSnapshot
	before   *ProtocolEndpointMutationSnapshot
	change   ProtocolEndpointMutationChange
}

func (s *protocolEndpointMutationRepositoryStub) LoadProtocolEndpointMutation(_ context.Context, actor, id uint) (ProtocolEndpointMutationSnapshot, error) {
	if actor != 7 || id != s.snapshot.Endpoint.ID {
		return ProtocolEndpointMutationSnapshot{}, ErrProtocolEndpointNotFound
	}
	return s.snapshot, nil
}

func (s *protocolEndpointMutationRepositoryStub) CommitProtocolEndpointMutation(_ context.Context, actor uint, before *ProtocolEndpointMutationSnapshot, change ProtocolEndpointMutationChange) (ProtocolEndpointMutationResult, error) {
	if actor != 7 {
		return ProtocolEndpointMutationResult{}, ErrProtocolEndpointMutationPermission
	}
	s.before, s.change = before, change
	change.Endpoint.ID = 19
	return ProtocolEndpointMutationResult{ProtocolEndpoint: change.Endpoint, ProtocolEndpointChangeEffects: change.Effects}, nil
}

func TestProtocolEndpointMutationsLoadDecryptsAndPreparesAtomicChange(t *testing.T) {
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.UTC)
	repository := &protocolEndpointMutationRepositoryStub{snapshot: ProtocolEndpointMutationSnapshot{
		Endpoint: ProtocolEndpointRecord{
			ID: 19, NodeID: 2, Name: "before", RuntimeKey: "runtime", Protocol: "vless", Address: "old.example",
			Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerCiphertext: `enc:{"type":"vless","users":[]}`,
			ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true, SortOrder: 8,
			ManagedPrincipalReady: true, MieruPrincipalReady: true,
		}, ManagedCertificateID: 11, Version: "snapshot",
	}}
	service := ProtocolEndpointMutations{Repository: repository, Cipher: proxyPoolMutationCipher{}, Now: func() time.Time { return now }}
	snapshot, err := service.Load(context.Background(), 7, 19)
	if err != nil || snapshot.Endpoint.ServerConfig != `{"type":"vless","users":[]}` {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
	result, err := service.Save(context.Background(), 7, &snapshot, ProtocolEndpointMutationRequest{
		ID: 19, NodeID: 3, Name: " after ", Protocol: "VLESS", Address: " new.example ", Port: 8443,
		PublicPort: 9443, MultiplierMilli: 2000, ServerConfig: `{"type":"vless","users":[]}`,
		ClientConfig: `{"server":"new.example"}`, OptionalConfig: "", Tags: "",
		MembershipChanges: []ProtocolEndpointMembershipChange{
			{NodeGroupID: 9, ExpectedRevision: 2, Member: true},
			{NodeGroupID: 4, ExpectedRevision: 3, Member: false},
		}, CredentialProtocols: []string{"VLESS", "vless"},
	})
	if err != nil || result.ProtocolEndpoint.ID != 19 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	change := repository.change
	if repository.before == nil || change.Endpoint.RuntimeKey != "runtime" || change.Endpoint.SortOrder != 8 || !change.Endpoint.IsActive {
		t.Fatalf("preserved change=%+v", change)
	}
	if change.Endpoint.Name != "after" || change.Endpoint.Address != "new.example" || change.Endpoint.ServerCiphertext != `enc:{"type":"vless","users":[]}` || change.Endpoint.OptionalConfig != "{}" || change.Endpoint.Tags != "[]" {
		t.Fatalf("normalized change=%+v", change)
	}
	if change.Endpoint.ManagedPrincipalReady || change.Endpoint.MieruPrincipalReady || change.MembershipChanges[0].NodeGroupID != 4 || len(change.CredentialProtocols) != 1 || !change.Now.Equal(now) {
		t.Fatalf("owned change=%+v", change)
	}
	if change.Effects.Effect != ProtocolEndpointEffectCredentialPlacement || change.Effects.PublishStatus != ProtocolEndpointPublishQueued || len(change.Effects.AffectedNodeIDs) != 2 {
		t.Fatalf("effects=%+v", change.Effects)
	}
}

func TestProtocolEndpointMutationsRejectMismatchedSnapshotAndInvalidMembership(t *testing.T) {
	service := ProtocolEndpointMutations{Repository: &protocolEndpointMutationRepositoryStub{}, Cipher: proxyPoolMutationCipher{}}
	snapshot := ProtocolEndpointMutationSnapshot{Endpoint: ProtocolEndpointRecord{ID: 5}}
	_, err := service.Save(context.Background(), 7, &snapshot, ProtocolEndpointMutationRequest{ID: 6})
	if !errors.Is(err, ErrProtocolEndpointConflict) {
		t.Fatalf("mismatched snapshot error=%v", err)
	}
	_, err = service.Save(context.Background(), 7, nil, ProtocolEndpointMutationRequest{
		NodeID: 1, Name: "endpoint", Address: "edge.example", Protocol: "vless", Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerConfig: "{}",
		MembershipChanges: []ProtocolEndpointMembershipChange{{NodeGroupID: 3, ExpectedRevision: 1, Member: false}},
	})
	var validation *ProtocolEndpointMutationValidation
	if !errors.As(err, &validation) || validation.Fields["node_group_membership_changes"] == "" {
		t.Fatalf("membership validation=%+v error=%v", validation, err)
	}
}

func TestDirectProtocolEndpointPublishNodeIDsExcludesReconcileTargets(t *testing.T) {
	got := DirectProtocolEndpointPublishNodeIDs([]uint{8, 3, 8, 5}, []uint{3, 11})
	want := []uint{5, 8}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("direct=%v want=%v", got, want)
	}
}
