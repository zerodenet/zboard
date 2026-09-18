package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type nodeAdministrationRepositoryStub struct {
	created NodeCreateChange
	updated NodeUpdateRequest
	result  NodeAdministrationRecord
	err     error
}

func (s *nodeAdministrationRepositoryStub) LoadNodeAdministration(context.Context, uint, uint) (NodeAdministrationSnapshot, error) {
	return NodeAdministrationSnapshot{Node: s.result}, s.err
}
func (s *nodeAdministrationRepositoryStub) CreateNodeAdministration(_ context.Context, _ uint, change NodeCreateChange) (NodeAdministrationRecord, error) {
	s.created = change
	return s.result, s.err
}
func (s *nodeAdministrationRepositoryStub) UpdateNodeAdministration(_ context.Context, _ uint, request NodeUpdateRequest) (NodeAdministrationRecord, error) {
	s.updated = request
	return s.result, s.err
}
func (s *nodeAdministrationRepositoryStub) UpdateNodeSSHConfiguration(context.Context, uint, uint, NodeSSHConfigurationChange) (NodeAdministrationRecord, error) {
	return s.result, s.err
}
func (s *nodeAdministrationRepositoryStub) ResetNodeSSHHostKey(context.Context, uint, uint) error {
	return s.err
}
func (s *nodeAdministrationRepositoryStub) RotateNodeCredential(context.Context, uint, uint, NodeCredentialKind, NodeCredentialChange) error {
	return s.err
}
func (s *nodeAdministrationRepositoryStub) RevokeNodeCredential(context.Context, uint, uint, NodeCredentialKind, time.Time) error {
	return s.err
}
func (s *nodeAdministrationRepositoryStub) RecordNodeSSHVerification(context.Context, uint, uint, time.Time, bool) (NodeAdministrationRecord, error) {
	return s.result, s.err
}

func TestNodeAdministrationNormalizesCreateAndUpdate(t *testing.T) {
	repository := &nodeAdministrationRepositoryStub{result: NodeAdministrationRecord{ID: 7}}
	service := NodeAdministration{Repository: repository}
	created, err := service.Create(context.Background(), 1, NodeCreateChange{Name: " node ", Region: " region ", Address: " address ", Remark: " remark ", IsEnabled: true})
	if err != nil || created.ID != 7 || repository.created.Name != "node" || repository.created.Region != "region" || repository.created.Address != "address" || repository.created.Remark != "remark" || repository.created.SSHPort != 22 || repository.created.CommunicationProtocol != 1 {
		t.Fatalf("created=%+v change=%+v error=%v", created, repository.created, err)
	}
	name, region, lifecycle, enabled := " renamed ", " west ", "ACTIVE ", true
	updated, err := service.Update(context.Background(), 1, NodeUpdateRequest{ID: 7, Name: &name, Region: &region, LifecycleStatus: &lifecycle, IsEnabled: &enabled})
	if err != nil || updated.ID != 7 || *repository.updated.Name != "renamed" || *repository.updated.Region != "west" || *repository.updated.LifecycleStatus != "active" {
		t.Fatalf("updated=%+v request=%+v error=%v", updated, repository.updated, err)
	}
}

func TestNodeAdministrationRejectsInvalidLifecycleAndEmptyChanges(t *testing.T) {
	service := NodeAdministration{Repository: &nodeAdministrationRepositoryStub{}}
	for _, request := range []NodeUpdateRequest{{ID: 1}, {ID: 1, LifecycleStatus: stringPointerNetwork("invalid")}, {ID: 1, LifecycleStatus: stringPointerNetwork("retired"), IsEnabled: boolPointerNetwork(true)}} {
		_, err := service.Update(context.Background(), 1, request)
		var validation *NodeAdministrationValidation
		if !errors.As(err, &validation) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
	if _, err := (NodeAdministration{}).Create(context.Background(), 1, NodeCreateChange{Name: "node"}); !errors.Is(err, ErrNodeAdministrationUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func stringPointerNetwork(value string) *string { return &value }
func boolPointerNetwork(value bool) *bool       { return &value }
