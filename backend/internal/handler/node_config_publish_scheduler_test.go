package handler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContextMutexCancelsLockWait(t *testing.T) {
	lock := newContextMutex()
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatalf("take initial lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := lock.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait error = %v, want deadline exceeded", err)
	}
	lock.Unlock()
}
