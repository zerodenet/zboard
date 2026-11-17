package main

import (
	"errors"
	"flag"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/application"
	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
	"github.com/zerodenet/zboard/backend/internal/localworkers"
)

var projectionWorkersMax = flag.Int("projection-workers-max", 0, "maximum additional local projection processes (default disabled; SQLite at most 1, MySQL at most 4)")
var projectionWorkersConfirmedBenefit = flag.Bool("projection-workers-confirmed-benefit", false, "administrator declaration that this deployment's equal-budget standalone-worker benefit was measured; this is not automatic benchmark verification")
var projectionWorkersLockFile = flag.String("projection-workers-lock-file", "", "absolute private ownership file shared by every supervisor of this database on this host; never stolen or removed")
var projectionWorkersDatabasePath = flag.String("projection-workers-database-path", "", "existing local database storage directory to measure; remote/unobservable database storage cannot admit expansion")
var projectionWorkersInterval = flag.Duration("projection-workers-interval", 5*time.Second, "local resource and ready-backlog sampling interval (1s–1m)")
var projectionWorkersCooldown = flag.Duration("projection-workers-cooldown", time.Minute, "minimum interval between healthy capacity increases (10s–1h)")
var projectionWorkersIdleFor = flag.Duration("projection-workers-idle-for", 2*time.Minute, "ready queue idle period before process-local drain (at least cooldown)")
var projectionWorkersBacklogDelay = flag.Duration("projection-workers-backlog-delay", 10*time.Second, "age of a ready projection intent required for expansion")
var projectionWorkersDrain = flag.Duration("projection-workers-drain-timeout", 8*time.Second, "managed child's graceful drain budget before bounded process interruption (1s–1m)")
var projectionWorkersReserveMemory = flag.Uint64("projection-workers-reserve-memory-bytes", 1024<<20, "available host/cgroup memory reserved for core and plugins")
var projectionWorkersMemory = flag.Uint64("projection-workers-process-memory-bytes", 512<<20, "conservative memory reservation and child Go GC target; not a hard RSS limit")
var projectionWorkersReserveDisk = flag.Uint64("projection-workers-reserve-disk-bytes", 512<<20, "free bytes retained on each observed database/spool filesystem")
var projectionWorkersMaxCPU = flag.Float64("projection-workers-max-cpu", .75, "maximum observed host/cgroup CPU fraction; an additional full CPU must also fit below this ceiling")
var projectionWorkersMaxIOWait = flag.Float64("projection-workers-max-iowait", .1, "maximum host IO-wait fraction before reducing managed workers")
var projectionWorkersMaxDBLatency = flag.Duration("projection-workers-max-database-latency", 100*time.Millisecond, "maximum recent durable Claim/Finish latency before reducing managed workers")
var projectionWorkersMaxProjectionRuntime = flag.Duration("projection-workers-max-projection-runtime", 2*time.Second, "conservative projection storage work duration that suppresses expansion; not a measurement of SQL latency")
var projectionWorkerBackground = flag.Bool("projection-worker-background", false, "managed worker only: lower this process's CPU scheduling priority")
var projectionWorkerConnections = flag.Int("projection-worker-database-connections", 0, "worker only: further reduce this process's configured database connection limit (1–2; zero keeps existing manual mode)")

func projectionWorkerPolicy() localworkers.Policy {
	p := localworkers.DefaultPolicy()
	p.MaxWorkers, p.AdministratorConfirmedBenefit = *projectionWorkersMax, *projectionWorkersConfirmedBenefit
	p.Cooldown, p.IdleFor, p.BacklogDelay = *projectionWorkersCooldown, *projectionWorkersIdleFor, *projectionWorkersBacklogDelay
	p.ReserveMemoryBytes, p.WorkerMemoryBytes, p.ReserveDiskBytes = *projectionWorkersReserveMemory, *projectionWorkersMemory, *projectionWorkersReserveDisk
	p.MaxCPUFraction, p.MaxIOWaitFraction, p.MaxDatabaseLatency = *projectionWorkersMaxCPU, *projectionWorkersMaxIOWait, *projectionWorkersMaxDBLatency
	p.MaxProjectionRuntime = *projectionWorkersMaxProjectionRuntime
	return p
}

func validateProjectionSupervisorFlags(flags *flag.FlagSet, otherMode bool) error {
	orphanSupervisor, orphanWorker := false, false
	flags.Visit(func(value *flag.Flag) {
		if strings.HasPrefix(value.Name, "projection-workers-") && value.Name != "projection-workers-max" {
			orphanSupervisor = true
		}
		if value.Name == "projection-worker-background" || value.Name == "projection-worker-database-connections" {
			orphanWorker = true
		}
	})
	if orphanWorker && !*projectionWorker {
		return errors.New("managed projection worker options require projection-worker")
	}
	if *projectionWorkerConnections < 0 || *projectionWorkerConnections > 2 {
		return errors.New("projection-worker-database-connections must be between zero and two")
	}
	if *projectionWorkersMax == 0 {
		if orphanSupervisor {
			return errors.New("local projection supervisor options require a positive projection-workers-max")
		}
		return nil
	}
	if otherMode {
		return errors.New("local projection supervision can run only with the complete server")
	}
	if err := projectionWorkerPolicy().Validate(); err != nil {
		return err
	}
	if *projectionWorkersInterval < time.Second || *projectionWorkersInterval > time.Minute || *projectionWorkersDrain < time.Second || *projectionWorkersDrain > time.Minute {
		return errors.New("invalid local projection supervisor sampling or drain interval")
	}
	if !filepath.IsAbs(*projectionWorkersLockFile) || !filepath.IsAbs(*projectionWorkersDatabasePath) {
		return errors.New("local projection supervision requires explicit absolute ownership and local database storage paths")
	}
	return nil
}

func configuredProjectionSupervisor(c cfgpkg.Config) (*application.ProjectionSupervisorOptions, error) {
	if *projectionWorkersMax == 0 {
		return nil, nil
	}
	if c.DatabaseDriver == "sqlite" && *projectionWorkersMax > 1 {
		return nil, errors.New("SQLite supports at most one additional local projection process")
	}
	database, err := os.Stat(*projectionWorkersDatabasePath)
	if err != nil || !database.IsDir() {
		return nil, errors.New("configured local database storage directory is unavailable")
	}
	parent, err := os.Stat(filepath.Dir(*projectionWorkersLockFile))
	if err != nil || !parent.IsDir() {
		return nil, errors.New("local projection ownership parent directory must already exist")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, errors.New("local projection executable unavailable")
	}
	configuration, err := filepath.Abs(*configFile)
	if err != nil {
		return nil, errors.New("local projection configuration path unavailable")
	}
	launcher, err := localworkers.NewCommandLauncher(executable, configuration, *projectionWorkersDrain, *projectionWorkersMemory)
	if err != nil {
		return nil, err
	}
	paths := []string{*projectionWorkersDatabasePath}
	if !locallyObservableProjectionDatabase(c) {
		// An arbitrary local directory cannot attest to a remote database's
		// disk headroom, and private memory SQLite cannot be shared by children.
		// Preserve the complete core; the sampler reports unknown and never
		// admits expansion for this deployment topology.
		paths = []string{""}
	}
	if spool := c.ZeroEventSpoolConfig(); spool.Enabled {
		paths = append(paths, spool.Directory)
	}
	return &application.ProjectionSupervisorOptions{Policy: projectionWorkerPolicy(), Interval: *projectionWorkersInterval, DrainTimeout: *projectionWorkersDrain, LockPath: *projectionWorkersLockFile, Launcher: launcher, Resources: &localworkers.HostResources{StoragePaths: paths}}, nil
}

func locallyObservableProjectionDatabase(c cfgpkg.Config) bool {
	if c.DatabaseDriver == "sqlite" {
		base, query, _ := strings.Cut(c.DataSource, "?")
		parameters, err := url.ParseQuery(query)
		return err == nil && !strings.Contains(strings.ToLower(base), ":memory:") && strings.ToLower(parameters.Get("mode")) != "memory"
	}
	if c.DatabaseDriver != "mysql" {
		return false
	}
	dsn, err := mysqlconfig.ParseDSN(c.DataSource)
	if err != nil {
		return false
	}
	if dsn.Net == "unix" {
		return filepath.IsAbs(dsn.Addr)
	}
	if dsn.Net != "tcp" {
		return false
	}
	host, _, err := net.SplitHostPort(dsn.Addr)
	if err != nil {
		return false
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func applyManagedProjectionWorkerLimits(c *cfgpkg.Config) error {
	if !*projectionWorker {
		return nil
	}
	if *projectionWorkerBackground {
		if err := localworkers.BackgroundPriority(); err != nil {
			return err
		}
	}
	if *projectionWorkerConnections > 0 {
		c.DatabaseMaxOpenConnections = min(c.DatabaseMaxOpenConnections, *projectionWorkerConnections)
		c.DatabaseMaxIdleConnections = min(c.DatabaseMaxIdleConnections, c.DatabaseMaxOpenConnections)
	}
	return nil
}
