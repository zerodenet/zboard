package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var backfillReportReceipts = flag.Bool("backfill-report-receipts", false, "register one bounded range of existing metering report identities; no quota replay, source deletion or HTTP")
var backfillReportAfter = flag.Uint("backfill-report-receipts-after", 0, "exclusive existing ledger ID lower bound")
var backfillReportThrough = flag.Uint("backfill-report-receipts-through", 0, "required inclusive existing ledger ID upper bound")
var backfillReportBatch = flag.Int("backfill-report-receipts-batch", 200, "maximum source rows to inspect in this invocation (1–200)")

func validateReportBackfillFlags(enabled bool, flags *flag.FlagSet) error {
	if enabled {
		return nil
	}
	orphan := false
	flags.Visit(func(value *flag.Flag) {
		if strings.HasPrefix(value.Name, "backfill-report-receipts-") {
			orphan = true
		}
	})
	if orphan {
		return errors.New("report receipt range options require backfill-report-receipts")
	}
	return nil
}

func validateReportBackfillMode(enabled, migrate, rollback, check, worker bool, after, through uint, batch int) error {
	if !enabled {
		if after != 0 || through != 0 || batch != 200 {
			return errors.New("report receipt range options require backfill-report-receipts")
		}
		return nil
	}
	if migrate || rollback || check || worker {
		return errors.New("backfill-report-receipts cannot be combined with another database execution mode")
	}
	return metering.ValidateReportReceiptBackfill(after, through, batch)
}

func runReportReceiptBackfill(ctx context.Context, c cfgpkg.Config, after, through uint, batch int) (out metering.ReportReceiptBackfillResult, err error) {
	options := datastore.PoolConfig{MaxOpenConnections: c.DatabaseMaxOpenConnections, MaxIdleConnections: c.DatabaseMaxIdleConnections, ConnectionLifetime: time.Duration(c.DatabaseConnectionMaxLifetimeSecs) * time.Second}
	// First use the existing noncreating connection. Missing SQLite paths and
	// unprepared recovery authority cannot turn into new empty databases.
	read, e := datastore.OpenProjectionCheckDatabase(ctx, c.DatabaseDriver, c.DataSource, options)
	if e != nil {
		return out, e
	}
	pool, e := read.DB()
	if e != nil {
		return out, errors.New("report receipt inspection pool unavailable")
	}
	if datastore.IsSQLite(read) {
		err = datastore.ValidatePreparedReportReceiptSchema(ctx, read)
	} else {
		err = read.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return datastore.ValidatePreparedReportReceiptSchema(ctx, tx) }, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	}
	err = errors.Join(err, pool.Close())
	if err != nil {
		return out, errors.New("report receipt recovery authority is not prepared")
	}
	db, e := datastore.OpenWithDriver(c.DatabaseDriver, c.DataSource, options)
	if e != nil {
		return out, errors.New("report receipt writer connection failed")
	}
	writerPool, e := db.DB()
	if e != nil {
		return out, errors.New("report receipt writer pool unavailable")
	}
	defer func() { err = errors.Join(err, writerPool.Close()) }()
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	// Deployment must still stop writers before DDL; this is not a live-DDL fence.
	out, err = application.BackfillMeteringReportReceipts(ctx, db, after, through, batch)
	if err != nil {
		return metering.ReportReceiptBackfillResult{}, errors.New("report receipt backfill was not confirmed; inspect or safely replay the same bounded range")
	}
	return out, nil
}
