package application

import (
	"database/sql"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

func (s *Services) trafficDatabase() *gorm.DB {
	s.trafficReadMu.RLock()
	defer s.trafficReadMu.RUnlock()
	if s.trafficReadDB != nil {
		return s.trafficReadDB
	}
	return s.Identity.db
}

// ConfigureTrafficReads owns the optional reporting connection. Startup closes
// only that extra view; the authority database remains owned by the process.
func (s *Services) ConfigureTrafficReads() (func() error, error) {
	view, closeView, err := datastore.OpenReadView(s.Identity.db)
	if err != nil {
		return nil, err
	}
	s.trafficReadMu.Lock()
	s.trafficReadDB = view
	s.trafficReadMu.Unlock()
	return func() error {
		s.trafficReadMu.Lock()
		if s.trafficReadDB == view {
			s.trafficReadDB = s.Identity.db
		}
		s.trafficReadMu.Unlock()
		return closeView()
	}, nil
}

func (s *Services) TrafficReadsUseSQLite() bool { return datastore.IsSQLite(s.Identity.db) }

// SetTrafficReadDatabaseForTest installs a session wrapper without exposing a
// production handler-owned connection field.
func (s *Services) SetTrafficReadDatabaseForTest(read *gorm.DB) {
	s.trafficReadMu.Lock()
	defer s.trafficReadMu.Unlock()
	s.trafficReadDB = read
}

type DatabasePoolStats struct {
	Accounting, Traffic sql.DBStats
	Shared              bool
}

func (s *Services) DatabasePoolStats() (DatabasePoolStats, error) {
	writer, err := s.Identity.db.DB()
	if err != nil {
		return DatabasePoolStats{}, err
	}
	reader, err := s.trafficDatabase().DB()
	if err != nil {
		return DatabasePoolStats{}, err
	}
	if writer == nil || reader == nil {
		return DatabasePoolStats{}, errors.New("database pool is unavailable")
	}
	return DatabasePoolStats{Accounting: writer.Stats(), Traffic: reader.Stats(), Shared: writer == reader}, nil
}

// Read pools are configured by startup and point at the authority database.
func (s *Services) PrincipalTrends() metering.PrincipalTrends {
	return metering.PrincipalTrends{Repository: meteringstore.PrincipalTrends{DB: s.Identity.db, ReadDB: s.trafficDatabase()}}
}

func (s *Services) TrafficTrends(cache metering.TrafficTrendCache) metering.TrafficTrends {
	return metering.TrafficTrends{Repository: meteringstore.TrafficTrends{DB: s.Identity.db, ReadDB: s.trafficDatabase(), Cache: cache}}
}

func (s *Services) Reconciliation() metering.Reconciliation {
	return metering.Reconciliation{Repository: meteringstore.Reconciliation{DB: s.Identity.db, ReadDB: s.trafficDatabase()}}
}

func (s *Services) UsageSummary() metering.UsageSummary {
	return metering.UsageSummary{Repository: meteringstore.UsageSummary{DB: s.Identity.db, ReadDB: s.trafficDatabase()}}
}

func (s *Services) NodeSeries() metering.NodeSeries {
	return metering.NodeSeries{Repository: meteringstore.NodeSeries{DB: s.Identity.db, ReadDB: s.trafficDatabase()}}
}

func (s *Services) Records() metering.Records {
	return metering.Records{Repository: meteringstore.Records{DB: s.Identity.db, ReadDB: s.trafficDatabase()}}
}

func (s *Services) Usage(cache metering.StatisticsCache, incremental *meteringstore.IncrementalCache) metering.Usage {
	return metering.Usage{Repository: meteringstore.Usage{DB: s.Identity.db, ReadDB: s.trafficDatabase(), Statistics: meteringstore.UsageStatisticsReader{Cache: cache, Incremental: incremental}}}
}
