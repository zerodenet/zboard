package platformstore

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

func TestMigrationTargetCancellationRetainsClassificationWithoutCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted, closed := make(chan struct{}), make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		close(accepted)
		var b [1]byte
		conn.Read(b[:])
		close(closed)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	target := platform.MigrationTarget{TargetDriver: "mysql", TargetDataSource: "test:secret-marker@tcp(" + listener.Addr().String() + ")/test?readTimeout=2s"}
	go func() { done <- (MigrationTargets{}).Check(ctx, target) }()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("target was not probed")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, platform.ErrMigrationTargetConnection) {
			t.Fatal("cancellation classification lost", err)
		}
		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("target credential escaped error boundary")
		}
	case <-time.After(time.Second):
		t.Fatal("target check remained blocked")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("target probe socket not closed")
	}
}
