package handler

import (
	"database/sql"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

type databasePoolSnapshot struct {
	Maximum     int     `json:"maximum"`
	Open        int     `json:"open"`
	InUse       int     `json:"in_use"`
	Idle        int     `json:"idle"`
	WaitCount   int64   `json:"wait_count"`
	WaitSeconds float64 `json:"wait_seconds"`
}

type runtimeDiagnostics struct {
	AsOf          time.Time                  `json:"as_of"`
	StartedAt     time.Time                  `json:"started_at"`
	AccountingDB  databasePoolSnapshot       `json:"accounting_db"`
	TrafficReadDB databasePoolSnapshot       `json:"traffic_read_db"`
	SharedPool    bool                       `json:"shared_pool"`
	EventConsumer *zeroEventConsumerSnapshot `json:"event_consumer"`
	EventSpool    *zeroevent.Status          `json:"event_spool"`
}

func snapshotDatabasePool(stats sql.DBStats) databasePoolSnapshot {
	return databasePoolSnapshot{stats.MaxOpenConnections, stats.OpenConnections, stats.InUse, stats.Idle,
		stats.WaitCount, stats.WaitDuration.Seconds()}
}

func (h *handlers) runtimeDiagnostics() (runtimeDiagnostics, error) {
	pools, err := h.services.DatabasePoolStats()
	if err != nil {
		return runtimeDiagnostics{}, err
	}
	result := runtimeDiagnostics{AsOf: time.Now().UTC(), StartedAt: zboardProcessStartedAt, AccountingDB: snapshotDatabasePool(pools.Accounting),
		TrafficReadDB: snapshotDatabasePool(pools.Traffic), SharedPool: pools.Shared}
	if value, ok := zeroEventRuntimeRegistry.Load(h); ok {
		runtime := value.(*zeroEventRuntime)
		consumer, spool := runtime.metrics.snapshot(), runtime.spool.Status()
		result.EventConsumer, result.EventSpool = &consumer, &spool
	}
	return result, nil
}
