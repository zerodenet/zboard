package handler

import (
	"database/sql"
	"errors"
	"math"

	"github.com/zerodenet/zboard/backend/internal/datastore"
)

const trafficIncrementalKeyLimit = 200000

var errTrafficIncrementalCapacity = errors.New("traffic statistics snapshot capacity exceeded")

type trafficStatisticsGroup struct {
	At                             sql.NullString
	User, Subscription, Node, Rate sql.NullInt64
}

type trafficIncrementalState struct {
	version datastore.SQLiteLedgerVersion
	groups  map[trafficStatisticsGroup]struct{}
	ids     [4]map[int64]struct{}
	keys    int
	value   trafficUsageStatistics
}

func newTrafficIncrementalState(version datastore.SQLiteLedgerVersion) *trafficIncrementalState {
	state := &trafficIncrementalState{version: version, groups: make(map[trafficStatisticsGroup]struct{})}
	for i := range state.ids {
		state.ids[i] = make(map[int64]struct{})
	}
	return state
}

func sameTrafficLedgerPrefix(a, b datastore.SQLiteLedgerVersion) bool {
	return a.Schema == b.Schema && a.Revision == b.Revision && a.Instance == b.Instance
}

func (s *trafficIncrementalState) add(group trafficStatisticsGroup, endpoint, raw, used sql.NullInt64, limit int) error {
	if _, exists := s.groups[group]; !exists {
		if s.keys >= limit {
			return errTrafficIncrementalCapacity
		}
		s.groups[group] = struct{}{}
		s.keys++
		s.value.Total++
	}
	for i, id := range [4]sql.NullInt64{group.User, group.Subscription, group.Node, endpoint} {
		if !id.Valid || (i == 1 && id.Int64 == 0) {
			continue
		}
		if _, exists := s.ids[i][id.Int64]; !exists {
			if s.keys >= limit {
				return errTrafficIncrementalCapacity
			}
			s.ids[i][id.Int64] = struct{}{}
			s.keys++
		}
	}
	for _, sum := range []struct {
		value *int64
		delta int64
	}{{&s.value.Aggregates.RawBytes, raw.Int64}, {&s.value.Aggregates.UsedBytes, used.Int64}} {
		if (sum.delta > 0 && *sum.value > math.MaxInt64-sum.delta) || (sum.delta < 0 && *sum.value < math.MinInt64-sum.delta) {
			return errors.New("traffic statistics integer overflow")
		}
		*sum.value += sum.delta
	}
	s.value.Aggregates.UserCount = int64(len(s.ids[0]))
	s.value.Aggregates.SubscriptionCount = int64(len(s.ids[1]))
	s.value.Aggregates.NodeCount = int64(len(s.ids[2]))
	s.value.Aggregates.ProtocolEndpointCount = int64(len(s.ids[3]))
	return nil
}
