package network

import (
	"context"
	"testing"
)

type publicationRequestRepositoryStub struct{ requests []PublicationRequest }

func (s *publicationRequestRepositoryStub) QueuePublicationRequests(_ context.Context, requests []PublicationRequest) error {
	s.requests = requests
	return nil
}

func TestPublicationRequestsDeduplicatesAndOrdersNodes(t *testing.T) {
	repository := &publicationRequestRepositoryStub{}
	err := (PublicationRequests{Repository: repository}).Queue(context.Background(), []PublicationRequest{
		{NodeID: 2, TriggerEndpointID: 20}, {NodeID: 0}, {NodeID: 1, TriggerEndpointID: 10}, {NodeID: 2, TriggerEndpointID: 21},
	})
	if err != nil || len(repository.requests) != 2 || repository.requests[0].NodeID != 1 || repository.requests[1].NodeID != 2 || repository.requests[1].TriggerEndpointID != 20 {
		t.Fatalf("requests=%+v error=%v", repository.requests, err)
	}
}
