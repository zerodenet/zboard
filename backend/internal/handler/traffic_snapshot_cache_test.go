package handler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestTrafficSnapshotExpiryAndErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache trafficSnapshotCache[int]
		var key [32]byte
		loads := 0
		load := func() (int, error) { loads++; return loads, nil }
		for range 2 {
			if value, err := cache.get(context.Background(), key, load); value != 1 || err != nil {
				t.Fatalf("cached value=%d error=%v", value, err)
			}
		}
		time.Sleep(trafficSnapshotLifetime)
		if value, err := cache.get(context.Background(), key, load); value != 2 || err != nil {
			t.Fatalf("expired value=%d error=%v", value, err)
		}
		time.Sleep(trafficSnapshotLifetime)
		failure := errors.New("query failed")
		if _, err := cache.get(context.Background(), key, func() (int, error) { return 0, failure }); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if value, err := cache.get(context.Background(), key, load); value != 3 || err != nil {
			t.Fatalf("failure cached: value=%d error=%v", value, err)
		}
	})
}

func TestTrafficSnapshotCoalescesConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache trafficSnapshotCache[int]
		var loads atomic.Int32
		release := make(chan struct{})
		var readers sync.WaitGroup
		for range 20 {
			readers.Go(func() {
				value, err := cache.get(context.Background(), [32]byte{}, func() (int, error) {
					loads.Add(1)
					<-release
					return 42, nil
				})
				if value != 42 || err != nil {
					t.Errorf("value=%d error=%v", value, err)
				}
			})
		}
		synctest.Wait()
		if loads.Load() != 1 {
			t.Fatalf("concurrent scans=%d", loads.Load())
		}
		close(release)
		readers.Wait()
	})
}

func TestTrafficSnapshotCancellationDoesNotPoisonOtherReaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache trafficSnapshotCache[int]
		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		defer cancelLeader()
		go func() {
			_, err := cache.get(leaderCtx, [32]byte{}, func() (int, error) {
				<-leaderCtx.Done()
				return 0, leaderCtx.Err()
			})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("leader error=%v", err)
			}
		}()
		synctest.Wait()
		waiterCtx, cancelWaiter := context.WithCancel(context.Background())
		defer cancelWaiter()
		go func() {
			_, err := cache.get(waiterCtx, [32]byte{}, func() (int, error) {
				t.Error("cancelled waiter started a query")
				return 0, nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("waiter error=%v", err)
			}
		}()
		go func() {
			value, err := cache.get(context.Background(), [32]byte{}, func() (int, error) { return 7, nil })
			if value != 7 || err != nil {
				t.Errorf("live reader value=%d error=%v", value, err)
			}
		}()
		synctest.Wait()
		cancelWaiter()
		synctest.Wait()
		cancelLeader()
		synctest.Wait()
	})
}

func TestTrafficSnapshotBoundsBusyAndCompletedEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache trafficSnapshotCache[int]
		release := make(chan struct{})
		for i := range trafficSnapshotCapacity {
			go func() {
				_, _ = cache.get(context.Background(), [32]byte{byte(i)}, func() (int, error) { <-release; return i, nil })
			}()
		}
		synctest.Wait()
		if value, err := cache.get(context.Background(), [32]byte{255}, func() (int, error) { return 99, nil }); value != 99 || err != nil {
			t.Fatalf("busy bypass value=%d error=%v", value, err)
		}
		if len(cache.entries) != trafficSnapshotCapacity {
			t.Fatalf("busy cache grew: %d", len(cache.entries))
		}
		close(release)
		synctest.Wait()
		for i := range 100 {
			_, _ = cache.get(context.Background(), [32]byte{byte(i)}, func() (int, error) { return i, nil })
		}
		if len(cache.entries) != trafficSnapshotCapacity {
			t.Fatalf("completed cache grew: %d", len(cache.entries))
		}
	})
}
