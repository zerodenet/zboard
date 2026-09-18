package network

import (
	"context"
	"errors"
	"testing"
)

type kernelHistoryRepositoryStub struct {
	request KernelDetectionRequest
	limit   int
	result  KernelHistoryResult
}

func (s *kernelHistoryRepositoryStub) ReadKernelHistory(_ context.Context, request KernelDetectionRequest, limit int) (KernelHistoryResult, error) {
	s.request, s.limit = request, limit
	return s.result, nil
}

func TestKernelHistoryUsesBoundedDefaultPage(t *testing.T) {
	repository := &kernelHistoryRepositoryStub{result: KernelHistoryResult{Operations: []KernelOperation{{ID: 1}}}}
	result, err := (KernelHistory{Repository: repository}).Get(context.Background(), KernelDetectionRequest{NodeID: 3, ActorID: 4})
	if err != nil || len(result.Operations) != 1 || repository.limit != 20 || repository.request.ActorID != 4 {
		t.Fatalf("result=%+v limit=%d request=%+v err=%v", result, repository.limit, repository.request, err)
	}
}

func TestKernelHistoryRejectsMissingAuthority(t *testing.T) {
	if _, err := (KernelHistory{}).Get(context.Background(), KernelDetectionRequest{NodeID: 1, ActorID: 1}); !errors.Is(err, ErrKernelHistoryUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
