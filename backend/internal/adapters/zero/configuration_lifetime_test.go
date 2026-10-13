package zero

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type configurationLifetimeSession struct {
	kernelRemoteSessionStub
	started, cleanupAllowed, closed chan struct{}
	cleanupFinished, closedEarly    atomic.Bool
	closeOnce                       sync.Once
}

func (s *configurationLifetimeSession) RunWithLifetime(ctx context.Context, _ string, _ bool) (string, error) {
	close(s.started)
	<-ctx.Done()
	<-s.cleanupAllowed
	s.cleanupFinished.Store(true)
	// The apply may have committed before its acknowledgement was lost.
	return "ZBOARD_CONFIG_COMMITTED=" + strings.Repeat("a", 64), ctx.Err()
}

func (s *configurationLifetimeSession) Close() error {
	if !s.cleanupFinished.Load() {
		s.closedEarly.Store(true)
	}
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestPublicationCancellationWaitsForRemoteCleanupAndRetainsUncertainApply(t *testing.T) {
	session := &configurationLifetimeSession{started: make(chan struct{}), cleanupAllowed: make(chan struct{}), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var release sync.Once
	finish := func() { release.Do(func() { close(session.cleanupAllowed) }) }
	defer finish()
	type outcome struct {
		result ConfigurationPublishResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := (ConfigurationPublisher{Dialer: offlineKernelDialer{session: session}}).Publish(ctx, ConfigurationPublishRequest{
			DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "test-only",
		})
		done <- outcome{result, err}
	}()
	select {
	case <-session.started:
	case <-time.After(time.Second):
		t.Fatal("publication did not enter the managed remote lifetime")
	}
	cancel()
	select {
	case <-session.closed:
		t.Fatal("SSH closed before publication children finished cleanup")
	case <-time.After(30 * time.Millisecond):
	}
	finish()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || !result.result.Committed || !result.result.MayHaveApplied || session.closedEarly.Load() {
			t.Fatalf("lost acknowledgement misclassified: %+v error=%v closed_early=%v", result.result, result.err, session.closedEarly.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("publication did not finish after remote cleanup")
	}
}
