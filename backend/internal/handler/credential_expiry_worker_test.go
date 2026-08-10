package handler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialExpiryWorkerRunsImmediatelyAndPeriodically(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCredentialExpiryWorker(ctx, 5*time.Millisecond, func(time.Time) error {
			if calls.Add(1) >= 2 {
				cancel()
			}
			return nil
		}, nil)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("credential expiry worker did not stop")
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("worker calls = %d, want at least 2", got)
	}
}

func TestCredentialExpiryWorkerReportsErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	want := errors.New("boom")
	reported := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCredentialExpiryWorker(ctx, time.Hour, func(time.Time) error {
			cancel()
			return want
		}, func(err error) {
			reported <- err
		})
	}()

	select {
	case err := <-reported:
		if !errors.Is(err, want) {
			t.Fatalf("reported error = %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("credential expiry worker did not report reconciliation error")
	}
	<-done
}
