package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type publicationStoreStub struct {
	item          Publication
	claimed       bool
	completed     error
	completionErr error
}

func (s *publicationStoreStub) Pending(context.Context, time.Time) (bool, error) {
	return s.claimed, nil
}
func (s *publicationStoreStub) Claim(context.Context, time.Time) (Publication, bool, error) {
	return s.item, s.claimed, nil
}
func (s *publicationStoreStub) Renew(context.Context, Publication, time.Time) (bool, error) {
	return true, nil
}
func (s *publicationStoreStub) Complete(_ context.Context, _ Publication, _ time.Time, failure error) error {
	s.completed = failure
	return s.completionErr
}

type publicationExecutorFunc func(context.Context, Publication) error

func (f publicationExecutorFunc) PublishNodeConfiguration(ctx context.Context, item Publication) error {
	return f(ctx, item)
}

func TestPublicationQueueCompletesExternalResult(t *testing.T) {
	want := errors.New("offline")
	store := &publicationStoreStub{item: Publication{NodeID: 1, Generation: 2, LeaseToken: "lease"}, claimed: true}
	queue := PublicationQueue{Store: store, Executor: publicationExecutorFunc(func(_ context.Context, item Publication) error {
		if item.NodeID != 1 {
			t.Fatal(item)
		}
		return want
	})}
	item, ok, err := queue.Claim(context.Background(), time.Now())
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := queue.Execute(context.Background(), item); !errors.Is(err, want) || !errors.Is(store.completed, want) {
		t.Fatalf("external result was not completed: execute=%v completed=%v", err, store.completed)
	}
}

func TestPublicationQueueFinalizesAfterParentCancellation(t *testing.T) {
	store := &publicationStoreStub{}
	ctx, cancel := context.WithCancel(context.Background())
	queue := PublicationQueue{Store: store, Executor: publicationExecutorFunc(func(ctx context.Context, _ Publication) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})}
	err := queue.Execute(ctx, Publication{NodeID: 1, Generation: 1, LeaseToken: "lease"})
	if !errors.Is(err, context.Canceled) || !errors.Is(store.completed, context.Canceled) {
		t.Fatalf("canceled result not finalized: execute=%v completed=%v", err, store.completed)
	}
}

func TestPublicationQueueClassifiesFinalizeFailure(t *testing.T) {
	completionErr := errors.New("ack unavailable")
	store := &publicationStoreStub{completionErr: completionErr}
	queue := PublicationQueue{Store: store, Executor: publicationExecutorFunc(func(context.Context, Publication) error { return nil })}
	err := queue.Execute(context.Background(), Publication{NodeID: 1, Generation: 1, LeaseToken: "lease"})
	if !errors.Is(err, ErrPublicationFinalize) || !errors.Is(err, completionErr) {
		t.Fatalf("finalize error was not classified: %v", err)
	}
}

func TestPublicationRetryDelayIsBounded(t *testing.T) {
	if PublicationRetryDelay(0) != 5*time.Second || PublicationRetryDelay(1) != 10*time.Second || PublicationRetryDelay(100) != 5*time.Minute {
		t.Fatal("unexpected publication retry policy")
	}
}
