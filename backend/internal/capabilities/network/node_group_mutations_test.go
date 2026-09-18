package network

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type nodeGroupMutationRepositoryStub struct {
	snapshot NodeGroupMutationSnapshot
	change   NodeGroupMutationChange
	before   *NodeGroupMutationSnapshot
	result   NodeGroupMutationResult
	err      error
}

func (s *nodeGroupMutationRepositoryStub) LoadNodeGroupMutation(context.Context, uint, uint) (NodeGroupMutationSnapshot, error) {
	return s.snapshot, s.err
}

func (s *nodeGroupMutationRepositoryStub) CommitNodeGroupMutation(_ context.Context, _ uint, before *NodeGroupMutationSnapshot, change NodeGroupMutationChange) (NodeGroupMutationResult, error) {
	s.before, s.change = before, change
	return s.result, s.err
}

func TestNodeGroupMutationsNormalizeCreateAndUpdate(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	repository := &nodeGroupMutationRepositoryStub{result: NodeGroupMutationResult{NodeGroup: NodeGroupRecord{ID: 9}}}
	service := NodeGroupMutations{Repository: repository, Now: func() time.Time { return now }}
	disabled := false
	result, err := service.Create(context.Background(), 4, NodeGroupCreateRequest{
		Name: " Group ", Code: " CODE ", Description: " detail ", IsEnabled: &disabled,
		ProtocolEndpointIDs: []uint{3, 0, 3, 2}, NetworkEntryIDs: []uint{8, 8},
		CredentialProtocols: []string{"VLESS", "vless", "trojan"},
	})
	if err != nil || result.NodeGroup.ID != 9 || repository.before != nil {
		t.Fatalf("result=%+v before=%+v error=%v", result, repository.before, err)
	}
	if got := repository.change.Group; got.Name != "Group" || got.Code != "code" || got.Description != "detail" || got.IsEnabled || !reflect.DeepEqual(got.ProtocolEndpointIDs, []uint{3, 2}) || !reflect.DeepEqual(got.NetworkEntryIDs, []uint{8}) {
		t.Fatalf("change=%+v", repository.change)
	}
	if !repository.change.Now.Equal(now) || !reflect.DeepEqual(repository.change.CredentialProtocols, []string{"trojan", "vless"}) {
		t.Fatalf("change=%+v", repository.change)
	}

	revision := uint64(3)
	repository.snapshot = NodeGroupMutationSnapshot{Group: NodeGroupRecord{ID: 9, Name: "old", Code: "old", IsEnabled: true, Revision: revision, ProtocolEndpointIDs: []uint{1}}}
	name := " Next "
	empty := []uint{}
	_, err = service.Update(context.Background(), 4, NodeGroupUpdateRequest{ID: 9, ExpectedRevision: &revision, Name: &name, ProtocolEndpointIDs: &empty})
	if err != nil || repository.before == nil || repository.change.Group.Name != "Next" || !repository.change.ReplaceEndpoints || len(repository.change.Group.ProtocolEndpointIDs) != 0 {
		t.Fatalf("before=%+v change=%+v error=%v", repository.before, repository.change, err)
	}
}

func TestNodeGroupMutationsExposeRevisionPreconditionAndConflict(t *testing.T) {
	repository := &nodeGroupMutationRepositoryStub{snapshot: NodeGroupMutationSnapshot{Group: NodeGroupRecord{ID: 5, Revision: 7}}}
	service := NodeGroupMutations{Repository: repository}
	if _, err := service.Update(context.Background(), 1, NodeGroupUpdateRequest{ID: 5}); err == nil {
		t.Fatal("missing revision accepted")
	} else {
		var precondition *NodeGroupMutationPrecondition
		if !errors.As(err, &precondition) || precondition.CurrentRevision != 7 {
			t.Fatalf("error=%v", err)
		}
	}
	stale := uint64(6)
	if _, err := service.Update(context.Background(), 1, NodeGroupUpdateRequest{ID: 5, ExpectedRevision: &stale, Description: stringPointer("x")}); err == nil {
		t.Fatal("stale revision accepted")
	} else {
		var conflict *NodeGroupMutationConflict
		if !errors.As(err, &conflict) || conflict.CurrentRevision != 7 {
			t.Fatalf("error=%v", err)
		}
	}
}

func stringPointer(value string) *string { return &value }
