package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type connectorActivityRepositoryStub struct {
	values []*time.Time
	err    error
	reads  int
}

func (s *connectorActivityRepositoryStub) ReadConnectorLastSeen(context.Context, uint) (*time.Time, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	if len(s.values) == 0 {
		return nil, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

func TestConnectorActivityObserverWaitsForFreshActivity(t *testing.T) {
	activatedAt := time.Now().UTC()
	stale, fresh := activatedAt.Add(-time.Second), activatedAt.Add(time.Second)
	repository := &connectorActivityRepositoryStub{values: []*time.Time{&stale, &fresh}}
	observer := ConnectorActivityObserver{Repository: repository, Timeout: time.Second, PollInterval: time.Millisecond}
	actual, err := observer.Wait(context.Background(), 4, activatedAt)
	if err != nil || !actual.Equal(fresh) || repository.reads != 2 {
		t.Fatalf("actual=%s reads=%d err=%v", actual, repository.reads, err)
	}
}

func TestConnectorActivityObserverDistinguishesTimeoutAndCancellation(t *testing.T) {
	activatedAt := time.Now().UTC()
	observer := ConnectorActivityObserver{Repository: &connectorActivityRepositoryStub{}, Timeout: 5 * time.Millisecond, PollInterval: time.Millisecond}
	if _, err := observer.Wait(context.Background(), 4, activatedAt); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := observer.Wait(ctx, 4, activatedAt); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}
