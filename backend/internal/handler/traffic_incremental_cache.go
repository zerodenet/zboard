package handler

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

const trafficIncrementalEntries = 3

// Display-only, bounded working sets for repeated range statistics. The outer
// two-second snapshot cache still owns freshness and request singleflight. Busy
// slots cannot be evicted: excess scopes use the complete SQL path without
// allocating another large working set or delaying unrelated scopes.
type trafficIncrementalCache struct {
	mu       sync.Mutex
	clock    uint64
	entries  map[[32]byte]*trafficIncrementalEntry
	keyLimit int // Zero selects the production bound; tests can exercise small limits.
}

type trafficIncrementalEntry struct {
	busy     bool
	used     uint64
	state    *trafficIncrementalState
	oversize *datastore.SQLiteLedgerVersion
}

func (c *trafficIncrementalCache) acquire(key [32]byte) *trafficIncrementalEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[[32]byte]*trafficIncrementalEntry)
	}
	entry := c.entries[key]
	if entry != nil && entry.busy {
		return nil
	}
	if entry == nil {
		if len(c.entries) >= trafficIncrementalEntries {
			var oldest [32]byte
			var selected *trafficIncrementalEntry
			for k, candidate := range c.entries {
				if !candidate.busy && (selected == nil || candidate.used < selected.used) {
					oldest, selected = k, candidate
				}
			}
			if selected == nil {
				return nil
			}
			delete(c.entries, oldest)
		}
		entry = &trafficIncrementalEntry{}
		c.entries[key] = entry
	}
	c.clock++
	entry.used, entry.busy = c.clock, true
	return entry
}

func (c *trafficIncrementalCache) load(base *gorm.DB, bucket trafficUsageBucketSpec, key [32]byte) (trafficUsageStatistics, bool, error) {
	var result trafficUsageStatistics
	entry := c.acquire(key)
	if entry == nil {
		return result, false, nil
	}
	defer func() { c.mu.Lock(); entry.busy = false; c.mu.Unlock() }()
	limit := c.keyLimit
	if limit <= 0 {
		limit = trafficIncrementalKeyLimit
	}
	var version datastore.SQLiteLedgerVersion
	used := false
	asOf := time.Now().UTC()
	err := base.Transaction(func(tx *gorm.DB) error {
		root := tx.Session(&gorm.Session{NewDB: true})
		var supported bool
		var err error
		version, supported, err = datastore.ReadSQLiteLedgerVersion(root)
		if err != nil || !supported {
			entry.state, entry.oversize = nil, nil
			return err
		}
		if entry.oversize != nil && sameTrafficLedgerPrefix(*entry.oversize, version) {
			return nil // Append-only growth cannot shrink an oversized key set.
		}
		state := entry.state
		rebuild := state == nil || !sameTrafficLedgerPrefix(state.version, version) ||
			(state.version.MaxID.Valid && (!version.MaxID.Valid || version.MaxID.Int64 < state.version.MaxID.Int64))
		if !rebuild && state.version.MaxID.Valid {
			var appended int64
			if err := root.Raw("SELECT COUNT(*) FROM traffic_records WHERE id > ? AND id <= ?", state.version.MaxID.Int64, version.MaxID.Int64).Row().Scan(&appended); err != nil {
				return err
			}
			rebuild = version.Rows != state.version.Rows+appended
		}
		if rebuild {
			state = newTrafficIncrementalState(version)
		} else if state.version.MaxID.Valid {
			tx = tx.Where("id > ?", state.version.MaxID.Int64)
		}
		entry.state, entry.oversize = state, nil
		if version.MaxID.Valid {
			rows, err := tx.Where("id <= ?", version.MaxID.Int64).Select(bucket.Expression +
				", user_id, COALESCE(subscription_id, 0), node_id, protocol_multiplier_milli, protocol_endpoint_id, raw_bytes, used_bytes").Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var group trafficStatisticsGroup
				var endpoint, raw, used sql.NullInt64
				if err := rows.Scan(&group.At, &group.User, &group.Subscription, &group.Node, &group.Rate, &endpoint, &raw, &used); err != nil {
					return err
				}
				if err := state.add(group, endpoint, raw, used, limit); err != nil {
					return err
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
		state.version = version
		state.value.AsOf, state.value.Bucket = asOf, bucket.Name
		result, used = state.value, true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		entry.state = nil // A partial scan or failed transaction is never reused.
		if errors.Is(err, errTrafficIncrementalCapacity) {
			entry.oversize = &version
			return trafficUsageStatistics{}, false, nil
		}
	}
	return result, used, err
}
