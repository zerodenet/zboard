package network

import (
	"context"
	"testing"
	"time"
)

type batchScopeRepositoryStub struct {
	nodes, protocols []uint
	groups           map[string][]uint
	nodeFilter       BatchNodeFilter
	protocolFilter   BatchProtocolFilter
}

func (s *batchScopeRepositoryStub) ResolveBatchNodes(_ context.Context, _ bool, _ []uint, filter BatchNodeFilter, _ time.Time, _ int) ([]uint, error) {
	s.nodeFilter = filter
	return s.nodes, nil
}
func (s *batchScopeRepositoryStub) ResolveBatchProtocols(_ context.Context, _ bool, _ []uint, filter BatchProtocolFilter, _ int) ([]uint, error) {
	s.protocolFilter = filter
	return s.protocols, nil
}
func (s *batchScopeRepositoryStub) GroupBatchProtocols(_ context.Context, _ []uint) ([]uint, map[string][]uint, error) {
	return s.nodes, s.groups, nil
}

func TestBatchScopesNormalizeValidateAndBoundTargets(t *testing.T) {
	repository := &batchScopeRepositoryStub{nodes: []uint{1}, protocols: []uint{2}}
	service := BatchScopes{Repository: repository}
	if _, err := service.Nodes(context.Background(), true, []uint{1}, BatchNodeFilter{}, time.Time{}, 10); err == nil {
		t.Fatal("node scope accepted explicit IDs with all_matching")
	}
	if _, err := service.Protocols(context.Background(), true, []uint{2}, BatchProtocolFilter{}, 10); err == nil {
		t.Fatal("protocol scope accepted explicit IDs with all_matching")
	}
	if _, err := service.Nodes(context.Background(), true, nil, BatchNodeFilter{Query: " EDGE ", LifecycleStatus: " ACTIVE ", KernelStatus: " healthy "}, time.Time{}, 10); err != nil {
		t.Fatal(err)
	}
	if repository.nodeFilter.Query != "edge" || repository.nodeFilter.LifecycleStatus != "active" || repository.nodeFilter.KernelStatus != "healthy" {
		t.Fatalf("node filter was not normalized: %+v", repository.nodeFilter)
	}
	if _, err := service.Protocols(context.Background(), true, nil, BatchProtocolFilter{Protocol: " VLESS ", DeploymentStatus: "broken"}, 10); err == nil {
		t.Fatal("invalid deployment status was accepted")
	}
	repository.nodes = []uint{1, 2}
	if _, _, err := service.GroupProtocols(context.Background(), []uint{3}, 1); err == nil {
		t.Fatal("grouped node scope exceeded its limit")
	}
}
