package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"gorm.io/gorm"
)

const (
	trafficSnapshotLifetime = 2 * time.Second
	trafficSnapshotCapacity = 32
)

// These snapshots are display-only. Accounting, access checks and live pages
// never consult this cache. Values must remain immutable after publication.
// The zero value is ready for use and belongs to one handlers/database instance.
type trafficSnapshotCache[T any] struct {
	mu      sync.Mutex
	entries map[[32]byte]*trafficSnapshotEntry[T]
}

type trafficSnapshotEntry[T any] struct {
	ready   chan struct{}
	expires time.Time
	value   T
	err     error
}

func trafficSnapshotQueryKey(query *gorm.DB, kind string) [32]byte {
	return meteringstore.UsageSnapshotQueryKey(query, kind)
}

func (c *trafficSnapshotCache[T]) get(ctx context.Context, key [32]byte, load func() (T, error)) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		if c.entries == nil {
			c.entries = make(map[[32]byte]*trafficSnapshotEntry[T])
		}
		if entry := c.entries[key]; entry != nil {
			select {
			case <-entry.ready:
				if time.Now().Before(entry.expires) {
					c.mu.Unlock()
					return entry.value, entry.err
				}
				delete(c.entries, key)
			default:
				c.mu.Unlock()
				select {
				case <-ctx.Done():
					return zero, ctx.Err()
				case <-entry.ready:
					// A cancelled leader must not cancel another live request.
					if errors.Is(entry.err, context.Canceled) || errors.Is(entry.err, context.DeadlineExceeded) {
						continue
					}
					return entry.value, entry.err
				}
			}
		}
		if len(c.entries) >= trafficSnapshotCapacity {
			var oldestKey [32]byte
			var oldest *trafficSnapshotEntry[T]
			for candidateKey, candidate := range c.entries {
				select {
				case <-candidate.ready:
					if oldest == nil || candidate.expires.Before(oldest.expires) {
						oldest, oldestKey = candidate, candidateKey
					}
				default:
				}
			}
			if oldest == nil {
				// All slots are busy: serve directly instead of accumulating
				// arbitrary filter combinations or blocking unrelated readers.
				c.mu.Unlock()
				return load()
			}
			delete(c.entries, oldestKey)
		}
		entry := &trafficSnapshotEntry[T]{ready: make(chan struct{}), expires: time.Now().Add(trafficSnapshotLifetime)}
		c.entries[key] = entry
		c.mu.Unlock()

		entry.value, entry.err = load()
		c.mu.Lock()
		if entry.err != nil {
			delete(c.entries, key)
		}
		close(entry.ready)
		c.mu.Unlock()
		return entry.value, entry.err
	}
}
