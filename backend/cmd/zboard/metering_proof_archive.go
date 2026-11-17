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

var archiveMeteringProof = flag.Bool("archive-metering-proof", false, "seal one explicit bounded range of old native proof material; leaves all billing facts, identities and cursors intact")
var restoreMeteringProof = flag.Uint("restore-metering-proof", 0, "restore exactly one sealed native proof to its existing ledger row; no quota or statistics replay")
var archiveMeteringProofAfter = flag.Uint("archive-metering-proof-after", 0, "exclusive ledger ID lower bound")
var archiveMeteringProofThrough = flag.Uint("archive-metering-proof-through", 0, "required inclusive ledger ID upper bound")
var archiveMeteringProofBefore = flag.String("archive-metering-proof-before", "", "required RFC3339 boundary at least seven days old")
var archiveMeteringProofBatch = flag.Int("archive-metering-proof-batch", 32, "maximum ledger rows inspected in this invocation (1..32)")

func validateMeteringProofFlags(fs *flag.FlagSet, otherMode bool) error {
	archive, restore := *archiveMeteringProof, *restoreMeteringProof != 0
	options, restoreVisited := false, false
	fs.Visit(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "archive-metering-proof-") {
			options = true
		}
		if f.Name == "restore-metering-proof" {
			restoreVisited = true
		}
	})
	if !archive && options {
		return errors.New("proof archive range options require archive-metering-proof")
	}
	if restoreVisited && !restore {
		return errors.New("restore-metering-proof requires a nonzero explicit ledger ID")
	}
	if archive && restore || (archive || restore) && otherMode {
		return errors.New("native proof maintenance modes cannot be combined with another execution mode")
	}
	if !archive {
		return nil
	}
	before, err := time.Parse(time.RFC3339Nano, *archiveMeteringProofBefore)
	if err != nil || before.After(time.Now().Add(-7*24*time.Hour)) || *archiveMeteringProofThrough == 0 || *archiveMeteringProofAfter > *archiveMeteringProofThrough || *archiveMeteringProofBatch < 1 || *archiveMeteringProofBatch > 32 {
		return errors.New("proof archive requires an explicit ID range, batch 1..32 and RFC3339 boundary at least seven days old")
	}
	return nil
}

func runMeteringProofMaintenance(ctx context.Context, c cfgpkg.Config) (out any, err error) {
	options := datastore.PoolConfig{MaxOpenConnections: c.DatabaseMaxOpenConnections, MaxIdleConnections: c.DatabaseMaxIdleConnections, ConnectionLifetime: time.Duration(c.DatabaseConnectionMaxLifetimeSecs) * time.Second}
	read, e := datastore.OpenProjectionCheckDatabase(ctx, c.DatabaseDriver, c.DataSource, options)
	if e != nil {
		return nil, e
	}
	pool, e := read.DB()
	if e != nil {
		return nil, errors.New("native proof inspection pool unavailable")
	}
	if datastore.IsSQLite(read) {
		err = datastore.ValidateTrafficMetaArchiveDatabase(read.WithContext(ctx))
	} else {
		err = read.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return datastore.ValidateTrafficMetaArchiveDatabase(tx) }, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	}
	err = errors.Join(err, pool.Close())
	if err != nil {
		return nil, errors.New("native proof authority is not prepared; no writer opened")
	}
	db, e := datastore.OpenWithDriver(c.DatabaseDriver, c.DataSource, options)
	if e != nil {
		return nil, errors.New("native proof writer connection failed")
	}
	writer, e := db.DB()
	if e != nil {
		return nil, errors.New("native proof writer pool unavailable")
	}
	defer func() { err = errors.Join(err, writer.Close()) }()
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	if *archiveMeteringProof {
		before, _ := time.Parse(time.RFC3339Nano, *archiveMeteringProofBefore)
		out, err = application.ArchiveMeteringProof(ctx, db, metering.ProofArchiveRequest{AfterID: *archiveMeteringProofAfter, ThroughID: *archiveMeteringProofThrough, Before: before, Limit: *archiveMeteringProofBatch})
	} else {
		out, err = application.RestoreMeteringProof(ctx, db, *restoreMeteringProof)
	}
	if err != nil {
		return nil, errors.New("native proof maintenance was not confirmed; inspect or safely replay the same explicit request")
	}
	return out, nil
}
