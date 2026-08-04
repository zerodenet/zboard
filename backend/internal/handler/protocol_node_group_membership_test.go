package handler

import (
	"errors"
	"reflect"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNormalizeProtocolEndpointNodeGroupMembershipChangesKeepsIncrementalCommandsDeterministic(t *testing.T) {
	got, err := normalizeProtocolEndpointNodeGroupMembershipChanges([]protocolEndpointNodeGroupMembershipChange{
		{NodeGroupID: 9, ExpectedRevision: 4, Member: false},
		{NodeGroupID: 3, ExpectedRevision: 7, Member: true},
	}, false)
	if err != nil {
		t.Fatalf("normalize changes: %v", err)
	}
	want := []protocolEndpointNodeGroupMembershipChange{
		{NodeGroupID: 3, ExpectedRevision: 7, Member: true},
		{NodeGroupID: 9, ExpectedRevision: 4, Member: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %#v, want %#v", got, want)
	}
}

func TestNormalizeProtocolEndpointNodeGroupMembershipChangesRejectsDuplicateAndCreateRemoval(t *testing.T) {
	for _, test := range []struct {
		name     string
		creating bool
		changes  []protocolEndpointNodeGroupMembershipChange
	}{
		{name: "duplicate", changes: []protocolEndpointNodeGroupMembershipChange{{NodeGroupID: 2, ExpectedRevision: 1, Member: true}, {NodeGroupID: 2, ExpectedRevision: 1, Member: false}}},
		{name: "create removal", creating: true, changes: []protocolEndpointNodeGroupMembershipChange{{NodeGroupID: 2, ExpectedRevision: 1, Member: false}}},
		{name: "missing revision", changes: []protocolEndpointNodeGroupMembershipChange{{NodeGroupID: 2, Member: true}}},
		{name: "too many", changes: func() []protocolEndpointNodeGroupMembershipChange {
			changes := make([]protocolEndpointNodeGroupMembershipChange, 101)
			for index := range changes {
				changes[index] = protocolEndpointNodeGroupMembershipChange{NodeGroupID: uint(index + 1), ExpectedRevision: 1, Member: true}
			}
			return changes
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeProtocolEndpointNodeGroupMembershipChanges(test.changes, test.creating)
			var validation *requestValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want requestValidationError", err)
			}
			if validation.fields["node_group_membership_changes"] == "" {
				t.Fatalf("validation fields = %#v", validation.fields)
			}
		})
	}
}

func TestNodeGroupMembershipChangedEndpointIDsDoesNotTreatStableLinksAsChanges(t *testing.T) {
	existing := []model.NodeGroupEndpoint{
		{ProtocolEndpointID: 7, SortOrder: 0},
		{ProtocolEndpointID: 9, SortOrder: 1},
	}
	if got := nodeGroupMembershipChangedEndpointIDs(existing, []uint{7, 9}); len(got) != 0 {
		t.Fatalf("stable membership changed IDs = %v, want none", got)
	}
	got := nodeGroupMembershipChangedEndpointIDs(existing, []uint{9, 11})
	if !reflect.DeepEqual(got, []uint{7, 11}) {
		t.Fatalf("changed IDs = %v, want [7 11]", got)
	}
}

func TestProtocolEndpointDirectPublishNodeIDsDefersCredentialSequencedNodes(t *testing.T) {
	got := protocolEndpointDirectPublishNodeIDs([]uint{8, 3, 8, 5}, []uint{3, 11})
	want := []uint{8, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct publish nodes = %v, want %v", got, want)
	}
}
