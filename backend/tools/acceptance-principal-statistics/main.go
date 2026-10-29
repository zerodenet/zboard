// Command acceptance-principal-statistics runs only an explicitly requested,
// isolated auxiliary-statistics experiment. All new databases and evidence stay.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const minimumFree uint64 = 512 * 1024 * 1024
const coreBytes = 1024

type options struct {
	execute, isolated                bool
	driver, root, dataRoot, timezone string
	sources, windows                 int
	minFree                          uint64
}
type batch struct {
	Phase        string  `json:"phase"`
	Rows         int     `json:"rows"`
	SQL          uint64  `json:"gorm_sql_statements"`
	ElapsedMS    float64 `json:"elapsed_ms"`
	ExpectedHeld bool    `json:"expected_held,omitempty"`
	Error        string  `json:"error,omitempty"`
}
type snapshot struct {
	Phase           string           `json:"phase"`
	Counts          map[string]int64 `json:"rows"`
	Logical         map[string]int64 `json:"logical_variable_bytes"`
	Physical        map[string]any   `json:"physical"`
	RSS             map[string]any   `json:"process_memory"`
	PointsSHA       string           `json:"immutable_points_sha256"`
	HourlySHA       string           `json:"hourly_sha256"`
	TrendsSHA       string           `json:"trends_sha256"`
	StatisticsState string           `json:"statistics_state"`
}
type summary struct {
	Version         int               `json:"version"`
	Status          string            `json:"status"`
	Executed        bool              `json:"executed"`
	Driver          string            `json:"driver"`
	Sources         int               `json:"sources"`
	Windows         int               `json:"windows"`
	Timezone        string            `json:"timezone"`
	Head            string            `json:"source_head"`
	Build           map[string]string `json:"build_provenance"`
	HelperSHA       string            `json:"helper_sha256"`
	SourceBefore    string            `json:"source_sha256_before"`
	SourceAfter     string            `json:"source_sha256_after"`
	SourceUnchanged bool              `json:"source_unchanged"`
	Dirty           bool              `json:"source_dirty"`
	ArtifactDir     string            `json:"artifact_dir,omitempty"`
	SummaryPath     string            `json:"summary_path,omitempty"`
	Database        map[string]any    `json:"database,omitempty"`
	Preflight       map[string]any    `json:"preflight"`
	Snapshots       []snapshot        `json:"snapshots,omitempty"`
	Batches         []batch           `json:"batches,omitempty"`
	ReplayCoverage  map[string]any    `json:"replay_coverage"`
	Checks          map[string]bool   `json:"checks"`
	Clock           map[string]any    `json:"clock"`
	Limits          []string          `json:"limitations"`
	Error           string            `json:"error,omitempty"`
}
type traceCounter struct{ n atomic.Uint64 }

func (t *traceCounter) LogMode(logger.LogLevel) logger.Interface    { return t }
func (*traceCounter) Info(context.Context, string, ...interface{})  {}
func (*traceCounter) Warn(context.Context, string, ...interface{})  {}
func (*traceCounter) Error(context.Context, string, ...interface{}) {}
func (t *traceCounter) Trace(_ context.Context, _ time.Time, _ func() (string, int64), _ error) {
	t.n.Add(1)
}
func main() { os.Exit(run()) }
func run() (exitCode int) {
	var o options
	flag.BoolVar(&o.execute, "execute", false, "explicitly create new isolated resources and run (default: plan only)")
	flag.BoolVar(&o.isolated, "isolated-mysql", false, "declare supplied loopback MySQL is a disposable acceptance server")
	flag.StringVar(&o.driver, "driver", "sqlite", "sqlite or mysql")
	flag.StringVar(&o.root, "artifact-root", "", "existing artifact parent OUTSIDE checkout; only a new random child is created")
	flag.StringVar(&o.dataRoot, "mysql-data-root", "", "accessible actual isolated MySQL data-volume mapping for separate disk budget")
	flag.StringVar(&o.timezone, "timezone", "Asia/Kathmandu", "IANA reporting calendar")
	flag.IntVar(&o.sources, "sources", 1024, "accepted scope sources,1000..100000")
	flag.IntVar(&o.windows, "windows", 32, "calendar windows,32..64")
	flag.Uint64Var(&o.minFree, "min-free-bytes", minimumFree, "reserve after estimated experiment; cannot be below512MiB")
	flag.Parse()
	r := summary{Version: 1, Status: "plan", Driver: o.driver, Sources: o.sources, Windows: o.windows, Timezone: o.timezone, Checks: map[string]bool{}, Preflight: map[string]any{}, Clock: map[string]any{}, Limits: []string{"auxiliary statistics experiment, not original P6 capacity gate or HTTP/finance/kernel E2E", "gorm SQL count excludes driver BEGIN/COMMIT and direct migration SQL", "process RSS is a sampled/high-water observation, not a resource reservation", "thin exact-time receipts and original principal/financial authority remain linear; no whole-database constant cap", "virtual retention clock is not a16-day live production soak", "new database/schema/artifacts are preserved; no cleanup or retry is performed"}}
	code := 2
	var secrets []string
	defer func() {
		if r.Error != "" {
			for _, secret := range secrets {
				if secret != "" {
					r.Error = strings.ReplaceAll(r.Error, secret, "<redacted>")
					for i := range r.Batches {
						r.Batches[i].Error = strings.ReplaceAll(r.Batches[i].Error, secret, "<redacted>")
					}
				}
			}
		}
		if r.SummaryPath != "" {
			if err := writeJSON(r.SummaryPath, r); err != nil {
				r.Status = "failed"
				r.Error = "cannot persist final summary"
				exitCode = 1
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
			exitCode = 1
		}
	}()
	fail := func(err error) int {
		r.Status = "failed"
		if !r.Executed {
			r.Status = "not_executed"
		}
		r.Error = err.Error()
		return code
	}
	if flag.NArg() != 0 || o.driver != "sqlite" && o.driver != "mysql" || o.sources < 1000 || o.sources > 100000 || o.windows < 32 || o.windows > 64 || o.minFree < minimumFree {
		return fail(errors.New("invalid bounded experiment parameters"))
	}
	loc, err := time.LoadLocation(o.timezone)
	if err != nil {
		return fail(errors.New("invalid reporting timezone"))
	}
	repo, err := repository()
	if err != nil {
		return fail(err)
	}
	r.Head, err = gitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return fail(err)
	}
	status, err := gitOutput(repo, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return fail(err)
	}
	r.Dirty = strings.TrimSpace(status) != ""
	r.SourceBefore, err = sourceDigest(repo)
	if err != nil {
		return fail(err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	r.HelperSHA, err = fileSHA(executable)
	if err != nil {
		return fail(err)
	}
	buildInfo, haveBuild := debug.ReadBuildInfo()
	r.Build = buildEvidence(buildInfo)
	// A plan reads source metadata only: no artifact directory or connection exists.
	if !o.execute {
		r.Preflight["execution_requires"] = []string{"clean checkout", "explicit-execute", "outside-checkout artifact parent", "512MiB minimum reserve plus estimated workload", "mysql additionally requires disposable declaration, empty-schema loopback DSN and actual data-volume mapping"}
		r.SourceAfter = r.SourceBefore
		r.SourceUnchanged = true
		return 0
	}
	if r.Dirty {
		return fail(errors.New("execution requires a clean committed source tree"))
	}
	if !haveBuild {
		return fail(errors.New("executable build provenance unavailable"))
	}
	if err := validateBuild(buildInfo, r.Head); err != nil {
		return fail(err)
	}
	r.Build["state"] = "verified_exact_clean_source"

	artifactRoot, err := filepath.Abs(o.root)
	if err != nil || o.root == "" {
		return fail(errors.New("explicit artifact root required"))
	}
	artifactRoot, err = filepath.EvalSymlinks(artifactRoot)
	if err != nil {
		return fail(errors.New("artifact parent must already exist"))
	}
	rel, err := filepath.Rel(repo, artifactRoot)
	if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fail(errors.New("artifact parent must be outside checkout"))
	}
	estimate := uint64(16*1024*1024) + uint64(o.sources)*(4*coreBytes+4096)
	artifactFree, err := freeBytes(artifactRoot)
	if err != nil {
		return fail(err)
	}
	r.Preflight["artifact_free_bytes"] = artifactFree
	r.Preflight["estimated_artifact_workload_bytes"] = estimate
	r.Preflight["required_post_estimate_reserve_bytes"] = o.minFree
	if o.minFree > ^uint64(0)-2*estimate-128*1024*1024 {
		return fail(errors.New("reserve/workload sum overflows"))
	}
	if artifactFree < o.minFree+estimate {
		return fail(errors.New("artifact disk does not satisfy P4 reserve plus estimated workload"))
	}
	var cfg *mysqldriver.Config
	if o.driver == "mysql" {
		if !o.isolated || o.dataRoot == "" {
			return fail(errors.New("MySQL requires explicit disposable declaration and data-volume mapping"))
		}
		dsn := os.Getenv("ZBOARD_ACCEPTANCE_MYSQL_DSN")
		if dsn == "" {
			return fail(errors.New("isolated acceptance MySQL DSN environment variable missing"))
		}
		secrets = append(secrets, dsn)
		cfg, err = isolatedConfig(dsn)
		if err != nil {
			return fail(err)
		}
		secrets = append(secrets, cfg.Passwd)

		dataFree, err := freeBytes(o.dataRoot)
		if err != nil {
			return fail(errors.New("cannot measure declared MySQL data-volume mapping"))
		}
		r.Preflight["mysql_data_free_bytes"] = dataFree
		r.Preflight["mysql_data_root"] = o.dataRoot
		r.Preflight["mysql_estimated_workload_bytes"] = estimate + 128*1024*1024
		if dataFree < o.minFree+estimate+128*1024*1024 {
			return fail(errors.New("MySQL data disk does not satisfy separate P4 reserve and redo/workload estimate"))
		}
		same, err := sameFilesystem(artifactRoot, o.dataRoot)
		if err != nil {
			return fail(err)
		}
		r.Preflight["artifact_and_mysql_same_filesystem"] = same
		if same {
			available := artifactFree
			if dataFree < available {
				available = dataFree
			}
			combined := 2*estimate + 128*1024*1024
			r.Preflight["combined_workload_estimate_bytes"] = combined
			if available < o.minFree+combined {
				return fail(errors.New("shared artifact/MySQL filesystem does not satisfy combined workload and P4 reserve"))
			}
		}

	}
	dir, err := os.MkdirTemp(artifactRoot, "principal-statistics-")
	if err != nil {
		return fail(err)
	}
	r.ArtifactDir = dir
	r.SummaryPath = filepath.Join(dir, "summary.json")
	r.Executed = true
	r.Status = "running"
	code = 1
	if err := writeJSON(filepath.Join(dir, "start.json"), r); err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var db *gorm.DB
	if cfg == nil {
		path := filepath.Join(dir, "isolated.sqlite")
		r.Database = map[string]any{"sqlite_path": path, "new_owned_database": true}
		db, err = datastore.OpenWithDriver("sqlite", path)
	} else {
		cfg.ParseTime = true
		cfg.Loc = time.UTC
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 30 * time.Second
		cfg.WriteTimeout = 30 * time.Second
		admin, openErr := sql.Open("mysql", cfg.FormatDSN())
		if openErr != nil {
			return fail(errors.New("open isolated MySQL administration connection failed"))
		}
		defer admin.Close()
		var token [12]byte
		if _, err = rand.Read(token[:]); err != nil {
			return fail(err)
		}
		schema := "zboard_principal_acceptance_" + hex.EncodeToString(token[:])
		r.Database = map[string]any{"schema": schema, "new_owned_database": true, "disposable_declaration": true, "data_volume_mapping": o.dataRoot}
		if err := writeJSON(filepath.Join(dir, "database-intent.json"), r.Database); err != nil {
			return fail(err)
		}
		if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
			return fail(fmt.Errorf("new schema CREATE outcome must be inspected, never retried: %w", err))
		}
		cfg.DBName = schema
		db, err = datastore.OpenWithDriver("mysql", cfg.FormatDSN())
	}
	if err != nil {
		return fail(errors.New("open new isolated database failed"))
	}
	pool, err := db.DB()
	if err != nil {
		return fail(err)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(2)
	trace := &traceCounter{}
	db = db.WithContext(ctx)
	db.Logger = trace
	if err = datastore.RunMigrations(db); err != nil {
		return fail(fmt.Errorf("actual migration failed: %w", err))
	}
	if err = experiment(ctx, db, trace, loc, o, &r); err != nil {
		return fail(err)
	}
	r.SourceAfter, err = sourceDigest(repo)
	if err != nil {
		return fail(err)
	}
	endHead, headErr := gitOutput(repo, "rev-parse", "HEAD")
	endStatus, statusErr := gitOutput(repo, "status", "--porcelain", "--untracked-files=normal")
	r.SourceUnchanged = r.SourceBefore == r.SourceAfter && headErr == nil && statusErr == nil && endHead == r.Head && endStatus == ""
	r.Checks["source_unchanged"] = r.SourceUnchanged
	if !r.SourceUnchanged {
		return fail(errors.New("source changed during execution; evidence cannot certify one source tree"))
	}

	finalFree, err := freeBytes(artifactRoot)
	if err != nil {
		return fail(err)
	}
	r.Preflight["artifact_final_free_bytes"] = finalFree
	if finalFree < o.minFree {
		return fail(errors.New("actual final artifact filesystem reserve fell below P4 floor"))
	}
	if o.driver == "mysql" {
		finalDataFree, err := freeBytes(o.dataRoot)
		if err != nil {
			return fail(err)
		}
		r.Preflight["mysql_data_final_free_bytes"] = finalDataFree
		if finalDataFree < o.minFree {
			return fail(errors.New("actual final MySQL filesystem reserve fell below P4 floor"))
		}
	}
	r.Status = "passed"
	return 0
}
func experiment(ctx context.Context, db *gorm.DB, trace *traceCounter, loc *time.Location, o options, r *summary) error {
	now := time.Now().UTC()
	base := now.Truncate(24*time.Hour).AddDate(0, 0, -o.windows-1)
	r.Clock = map[string]any{"source_created_at": "actual DB-persisted wall clock from RefreshScope; never backdated", "observed_from": base, "retention_now": "set after seeding to actual wall clock plus16days; explicit simulation only", "live_soak": false}
	user := model.User{AccountName: "principal-acceptance", Email: "principal-acceptance@example.invalid", Password: "isolated-unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		return err
	}
	node := model.Node{Name: "principal-acceptance", Address: "127.0.0.1", Region: "acceptance", AssetTag: strings.Repeat("a", 64), Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		return err
	}
	core := strings.Repeat("r", coreBytes)
	current := meteringstore.PrincipalFlowCurrent{NodeID: node.ID, PrincipalKey: "isolated-statistics", CoreInstanceID: core, SessionRegistryRevision: 1, UserID: user.ID, ActiveFlows: 5, ObservedAt: now, UpdatedAt: now}
	if err := db.Create(&current).Error; err != nil {
		return err
	}
	event := func(i int) string { return fmt.Sprintf("principal-acceptance-%08d", i) }
	observed := func(i int) time.Time {
		return base.AddDate(0, 0, i%o.windows).Add(12*time.Hour + time.Duration(i/o.windows)*time.Second)
	}
	refresh := func(id string, at time.Time) error {
		return db.Transaction(func(tx *gorm.DB) error {
			return meteringstore.RefreshScope(tx, "user", user.ID, node.ID, core, 1, id, at, "acceptance")
		})
	}
	local := base.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day()-1, 0, 0, 0, 0, loc)
	q := metering.PrincipalTrendQuery{UserID: user.ID}
	for i := 0; i < o.windows+3; i++ {
		start := from.AddDate(0, 0, i)
		q.Buckets = append(q.Buckets, metering.TrendBucket{Key: start.Format("2006-01-02"), StartUTC: start.UTC(), EndUTC: start.AddDate(0, 0, 1).UTC()})
	}
	snap := func(phase string) (snapshot, error) {
		s, err := capture(db, user.ID, q, r.Database, phase)
		if err == nil {
			r.Snapshots = append(r.Snapshots, s)
			err = writeJSON(filepath.Join(r.ArtifactDir, "snapshot-"+phase+".json"), s)
		}
		return s, err
	}

	empty, err := snap("empty")
	if err != nil {
		return err
	}
	if empty.Counts["raw"] != 0 || empty.Counts["point"] != 0 || empty.Counts["pending"] != 0 || empty.Counts["hour"] != 0 {
		return errors.New("isolated empty source baseline is not empty")
	}
	for from := 0; from < o.sources; from += 64 {
		start := time.Now()
		sqlBefore := trace.n.Load()
		end := from + 64
		if end > o.sources {
			end = o.sources
		}
		for i := from; i < end; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := refresh(event(i), observed(i)); err != nil {
				return fmt.Errorf("scope source %d: %w", i, err)
			}
		}
		r.Batches = append(r.Batches, batch{Phase: "source", Rows: end - from, SQL: trace.n.Load() - sqlBefore, ElapsedMS: ms(start)})
	}

	// Accepted durable sources are not yet a complete reporting snapshot. Prove
	// the real reporting capability returns pending, then collect storage only.
	if _, err := (meteringstore.PrincipalTrends{DB: db}).Read(ctx, user.ID, q); !errors.Is(err, metering.ErrUsageProjectionPending) {
		return fmt.Errorf("unprojected reporting did not stay unknown: %v", err)
	}
	accepted, err := snap("source_pending")
	if err != nil {
		return err
	}
	if accepted.Counts["raw"] != int64(o.sources) || accepted.Counts["pending"] != int64(o.sources) || accepted.Counts["point"] != 0 || accepted.Counts["hour"] != 0 {
		return errors.New("accepted durable pending baseline differs")
	}
	r.Checks["accepted_sources_reporting_pending"] = true
	worker := meteringstore.PrincipalStatistics{DB: db}
	for i := 0; i < o.sources/200+4; i++ {
		pending, err := worker.HasPending(ctx)
		if err != nil {
			return err
		}
		if !pending {
			break
		}
		start := time.Now()
		before := trace.n.Load()
		n, err := worker.Drain(ctx, 200)
		r.Batches = append(r.Batches, batch{Phase: "drain", Rows: n, SQL: trace.n.Load() - before, ElapsedMS: ms(start)})
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("pending sources made no bounded projection progress")
		}
	}
	if pending, err := worker.HasPending(ctx); err != nil || pending {
		return fmt.Errorf("projection not fully drained: %v", err)
	}
	baseline, err := snap("projected")
	if err != nil {
		return err
	}
	if baseline.Counts["raw"] != int64(o.sources) || baseline.Counts["point"] != int64(o.sources) || baseline.Counts["pending"] != 0 {
		return errors.New("new-source projection count mismatch")
	}
	var first, anchor struct{ ID uint64 }
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Select("id").Where("event_id=?", event(0)).Take(&first).Error; err != nil {
		return err
	}
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Select("id").Order("id DESC").Take(&anchor).Error; err != nil {
		return err
	}
	// Deliberately damage only this newly-created fixture's duplicate diagnostic
	// source. Its original sample/hash is not rewritten to bless damaged evidence.
	damaged := core + "-injected-corruption"
	if err := writeJSON(filepath.Join(r.ArtifactDir, "fault-intent.json"), map[string]any{"source_id": first.ID, "original_core_sha256": stringSHA(core), "injected_core_sha256": stringSHA(damaged), "scope": "new isolated diagnostic source only"}); err != nil {
		return err
	}
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Where("id=?", first.ID).Update("core_instance_id", damaged).Error; err != nil {
		return err
	}
	virtual := time.Now().UTC().Add(16 * 24 * time.Hour)
	r.Clock["retention_now"] = virtual
	r.Clock["original_anchor_id"] = anchor.ID
	retention := meteringstore.PrincipalScopeRetention{DB: db, Now: func() time.Time { return virtual }}
	retire := func(phase string) (int, error) {
		total := 0
		for i := 0; i < o.sources/32+8; i++ {
			pending, err := retention.HasPending(ctx)
			if err != nil {
				return total, err
			}
			if !pending {
				return total, nil
			}
			start := time.Now()
			before := trace.n.Load()
			n, err := retention.Retire(ctx, 32, 8*1024*1024)
			expected := err != nil && err.Error() == "principal source no longer matches complete statistical receipt; retained original"
			r.Batches = append(r.Batches, batch{Phase: phase, Rows: n, SQL: trace.n.Load() - before, ElapsedMS: ms(start), ExpectedHeld: expected, Error: errorMessage(err)})
			total += n
			if err != nil && !expected {
				return total, err
			}
			if n == 0 && !expected {
				return total, errors.New("bounded retirement made no progress")
			}
		}
		return total, errors.New("retirement exceeded finite fixture work budget")
	}
	retired, err := retire("retire")
	if err != nil {
		return err
	}
	if retired != o.sources-2 {
		return fmt.Errorf("retired %d sources, expected%d plus corrupt held and real anchor", retired, o.sources-2)
	}
	after, err := snap("retired")
	if err != nil {
		return err
	}
	if after.Counts["raw"] != 2 || !sameFacts(baseline, after) {
		return errors.New("retirement changed immutable thin facts/statistics or lost held/anchor")
	}
	var held struct {
		Core string `gorm:"column:core_instance_id"`
	}
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Select("core_instance_id").Where("id=?", first.ID).Take(&held).Error; err != nil || held.Core != damaged {
		return errors.New("corrupt original was deleted or rewritten")
	}
	var retainedAnchor int64
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Where("id=?", anchor.ID).Count(&retainedAnchor).Error; err != nil || retainedAnchor != 1 {
		return errors.New("real allocator anchor was lost")
	}
	r.Checks["corrupt_original_held"] = true
	r.Checks["real_allocator_anchor_retained"] = true
	r.Checks["retirement_preserves_thin_and_statistics_hashes"] = true
	// Replays use the actual original constant-count sources; no current state is
	// altered to fabricate old event values. The intentionally corrupt source is
	// retained as a fault and is excluded from legitimate replay inputs.
	start := time.Now()
	before := trace.n.Load()
	replayed := 0
	step := o.sources / 64
	if step < 1 {
		step = 1
	}
	for i := o.sources - 1; i > 0; i -= step {
		if err := refresh(event(i), observed(i)); err != nil {
			return fmt.Errorf("late/reverse replay source%d: %w", i, err)
		}
		replayed++
	}
	r.Batches = append(r.Batches, batch{Phase: "reverse_replay", Rows: replayed, SQL: trace.n.Load() - before, ElapsedMS: ms(start)})
	r.ReplayCoverage = map[string]any{"kind": "bounded_reverse_sample", "accepted_sources": o.sources, "replayed_sources": replayed, "covers_all_accepted_sources": false, "excluded_corrupt_sources": 1, "selection_step": step}
	replay, err := snap("replayed")
	if err != nil {
		return err
	}
	if !sameFacts(baseline, replay) || replay.Counts["raw"] != 2 || replay.Counts["pending"] != 0 {
		return errors.New("old replay changed statistics or recreated retired material")
	}
	r.Checks["old_reverse_replay_idempotent"] = true
	lateAt := observed(1).Add(500 * time.Millisecond)
	if err := refresh("principal-acceptance-distinct-late", lateAt); err != nil {
		return err
	}
	start = time.Now()
	before = trace.n.Load()
	n, err := worker.Drain(ctx, 200)
	r.Batches = append(r.Batches, batch{Phase: "late_drain", Rows: n, SQL: trace.n.Load() - before, ElapsedMS: ms(start)})
	if err != nil || n != 1 {
		return fmt.Errorf("late source failed exact once projection: %d %v", n, err)
	}
	late, err := snap("late_projected")
	if err != nil {
		return err
	}
	if late.Counts["point"] != int64(o.sources+1) || late.Counts["pending"] != 0 {
		return errors.New("distinct late source did not add exactly one point")
	}
	var count int64
	if err := db.Model(&model.PrincipalStatisticsHourly{}).Select("COALESCE(SUM(sample_count),0)").Scan(&count).Error; err != nil || count != int64(o.sources+1) {
		return fmt.Errorf("late source hourly count differs: %d %v", count, err)
	}
	if err := refresh("principal-acceptance-distinct-late", lateAt); err != nil {
		return err
	}
	lateReplay, err := snap("late_replayed")
	if err != nil {
		return err
	}
	if !sameFacts(late, lateReplay) {
		return errors.New("late source replay changed facts")
	}
	r.Checks["distinct_late_source_exactly_once"] = true
	// The former anchor may now retire; a new real late source remains pinned.
	if _, err := retire("retire_after_late"); err != nil {
		return err
	}
	final, err := snap("final")
	if err != nil {
		return err
	}
	if final.Counts["raw"] != 2 || !sameFacts(late, final) {
		return errors.New("final retention/allocator anchor differs")
	}
	if err := datastore.ValidatePreparedPrincipalStatisticsSchema(ctx, db); err != nil {
		return fmt.Errorf("final source recovery floor/authority validation: %w", err)
	}
	r.Checks["final_authority_and_allocator_floor_valid"] = true
	return nil
}
func capture(db *gorm.DB, actor uint, q metering.PrincipalTrendQuery, database map[string]any, phase string) (snapshot, error) {
	s := snapshot{Phase: phase, Counts: map[string]int64{}, Logical: map[string]int64{}, Physical: map[string]any{}, RSS: memory()}
	for key, table := range map[string]string{"raw": "principal_flow_scope_observations", "point": "principal_statistics_samples", "hour": "principal_statistics_hourly", "pending": "principal_statistics_pending", "state": "principal_statistics_state", "principal_authority": "principal_flow_observations", "financial_ledger": "traffic_records", "report_receipt": "metering_report_receipts", "usage_pending": "metering_usage_projection_pending", "observation_headers": "node_observation_intents", "observation_parts": "node_observation_parts"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil {
			return s, err
		}
		s.Counts[key] = n
	}
	length := func(column string) string {
		if db.Dialector.Name() == "sqlite" {
			return "length(CAST(" + column + " AS BLOB))"
		}
		return "OCTET_LENGTH(" + column + ")"
	}
	for key, item := range map[string]struct {
		table   string
		columns []string
	}{"raw": {"principal_flow_scope_observations", []string{"core_instance_id", "scope_type", "event_id", "source"}}, "point": {"principal_statistics_samples", []string{"scope_type", "event_id", "source_digest"}}, "hour": {"principal_statistics_hourly", []string{"scope_type"}}} {
		var terms []string
		for _, c := range item.columns {
			terms = append(terms, length(c))
		}
		var n int64
		if err := db.Table(item.table).Select("COALESCE(SUM(" + strings.Join(terms, "+") + "),0)").Scan(&n).Error; err != nil {
			return s, err
		}
		s.Logical[key] = n
	}
	var err error
	s.PointsSHA, err = pointSHA(db)
	if err != nil {
		return s, err
	}
	var hours []model.PrincipalStatisticsHourly
	// At most64 fixture windows plus boundary data; fixed engineering bound.
	if err := db.Order("scope_type,scope_id,hour_at").Limit(512).Find(&hours).Error; err != nil {
		return s, err
	}
	if int64(len(hours)) != s.Counts["hour"] {
		return s, errors.New("hourly fixture output exceeded bounded audit limit")
	}
	s.HourlySHA = jsonSHA(hours)

	if phase == "source_pending" {
		if s.Counts["pending"] < 1 {
			return s, errors.New("pending baseline lacks actual durable pending sources")
		}
		s.StatisticsState = "pending"
		s.TrendsSHA = "unknown"
	} else {
		s.StatisticsState = "complete"
		rows, err := (meteringstore.PrincipalTrends{DB: db}).Read(db.Statement.Context, actor, q)
		if err != nil {
			return s, err
		}
		s.TrendsSHA = jsonSHA(rows)
		var samples int64
		for _, row := range rows {
			samples += row.SampleCount
			if row.Peak != 5 {
				return s, errors.New("nonzero scope peak changed")
			}
		}
		if samples != s.Counts["point"] {
			return s, fmt.Errorf("calendar count%d differs from exact points%d", samples, s.Counts["point"])
		}

	}
	if db.Dialector.Name() == "sqlite" {
		var pages, size, free int64
		for statement, target := range map[string]*int64{"PRAGMA page_count": &pages, "PRAGMA page_size": &size, "PRAGMA freelist_count": &free} {
			if err := db.Raw(statement).Scan(target).Error; err != nil {
				return s, err
			}
		}
		s.Physical["page_count"] = pages
		s.Physical["page_size"] = size
		s.Physical["freelist_pages"] = free
		s.Physical["allocated_page_bytes"] = pages * size
		path, _ := database["sqlite_path"].(string)
		for _, suffix := range []string{"", "-wal", "-shm"} {
			info, err := os.Stat(path + suffix)
			if err == nil {
				s.Physical["file_bytes"+suffix] = info.Size()
			} else if !errors.Is(err, os.ErrNotExist) {
				return s, err
			} else {
				s.Physical["file_bytes"+suffix] = 0
			}
		}
		s.Physical["meaning"] = "current file/page allocation and reusable freelist; logical deletion does not imply filesystem shrink; no VACUUM/checkpoint forced"
	} else {
		var totals struct{ DataBytes, IndexBytes int64 }
		if err := db.Raw("SELECT COALESCE(SUM(DATA_LENGTH),0) AS data_bytes,COALESCE(SUM(INDEX_LENGTH),0) AS index_bytes FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&totals).Error; err != nil {
			return s, err
		}
		s.Physical["data_length_estimate_bytes"] = totals.DataBytes
		s.Physical["index_length_estimate_bytes"] = totals.IndexBytes
		var datadir string
		if err := db.Raw("SELECT @@datadir").Scan(&datadir).Error; err != nil {
			return s, err
		}
		s.Physical["server_datadir"] = datadir
		s.Physical["meaning"] = "INFORMATION_SCHEMA allocation estimates may be stale; shared redo/binlog/volume growth are not attributable table bytes; no ANALYZE/shrink/cleanup forced"
	}
	return s, nil
}
func pointSHA(db *gorm.DB) (string, error) {
	h := sha256.New()
	var after uint64
	for {
		var rows []model.PrincipalStatisticsSample
		if err := db.Select("observation_id,scope_type,scope_id,event_id,observed_at,active_flows,source_digest,source_created_at").Where("observation_id>?", after).Order("observation_id").Limit(200).Find(&rows).Error; err != nil {
			return "", err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			body, err := json.Marshal(struct {
				ID       uint64
				Scope    string
				ScopeID  uint
				Event    string
				Observed string
				Active   uint64
				Digest   string
				Created  string
			}{row.ObservationID, row.ScopeType, row.ScopeID, row.EventID, row.ObservedAt.UTC().Format(time.RFC3339Nano), row.ActiveFlows, row.SourceDigest, row.SourceCreatedAt.UTC().Format(time.RFC3339Nano)})
			if err != nil {
				return "", err
			}
			_, _ = h.Write(body)
			_, _ = h.Write([]byte{'\n'})
			after = row.ObservationID
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func sameFacts(a, b snapshot) bool {
	return a.StatisticsState == "complete" && b.StatisticsState == "complete" && a.PointsSHA == b.PointsSHA && a.HourlySHA == b.HourlySHA && a.TrendsSHA == b.TrendsSHA && a.Counts["point"] == b.Counts["point"] && a.Counts["hour"] == b.Counts["hour"]
}
func ms(start time.Time) float64 { return float64(time.Since(start).Nanoseconds()) / 1e6 }
func stringSHA(s string) string  { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func jsonSHA(v any) string       { raw, _ := json.Marshal(v); return stringSHA(string(raw)) }
func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeJSON(path string, value any) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(value)
	return errors.Join(err, f.Sync(), f.Close())
}
func repository() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return gitOutput(dir, "rev-parse", "--show-toplevel")
}
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("source git metadata unavailable")
	}
	return strings.TrimSpace(string(out)), nil
}
func sourceDigest(repo string) (string, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--", "backend")
	cmd.Dir = repo
	raw, err := cmd.Output()
	if err != nil {
		return "", errors.New("tracked source inventory unavailable")
	}
	names := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		if name == "" {
			continue
		}
		digest, err := fileSHA(filepath.Join(repo, name))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\n", name, digest)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func freeBytes(path string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, errors.New("cannot measure requested filesystem")
	}
	return fs.Bavail * uint64(fs.Bsize), nil
}
func memory() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	out := map[string]any{"go_heap_alloc_bytes": m.HeapAlloc, "rss_bytes": nil, "high_water_rss_bytes": nil, "rss_state": "unknown"}
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "VmRSS:":
			out["rss_bytes"] = n * 1024
			out["rss_state"] = "observed"
		case "VmHWM:":
			out["high_water_rss_bytes"] = n * 1024
		}
	}
	return out
}

// Parsing/admission performs no connection, file mutation or secret logging.
func isolatedConfig(dsn string) (*mysqldriver.Config, error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, errors.New("isolated DSN could not be parsed")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	ip := net.ParseIP(host)
	if cfg.Net != "tcp" || err != nil || ip == nil || !ip.IsLoopback() || cfg.DBName != "" || cfg.MultiStatements {
		return nil, errors.New("DSN must use literal loopback TCP, empty schema and no multiStatements")
	}
	return cfg, nil
}

func sameFilesystem(a, b string) (bool, error) {
	left, err := os.Stat(a)
	if err != nil {
		return false, errors.New("artifact filesystem identity unavailable")
	}
	right, err := os.Stat(b)
	if err != nil {
		return false, errors.New("data filesystem identity unavailable")
	}
	l, ok := left.Sys().(*syscall.Stat_t)
	rr, ok2 := right.Sys().(*syscall.Stat_t)
	if !ok || !ok2 {
		return false, errors.New("filesystem device identity unavailable; cannot prove separate budgets")
	}
	return l.Dev == rr.Dev, nil
}
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func buildEvidence(info *debug.BuildInfo) map[string]string {
	out := map[string]string{"state": "unknown"}
	if info == nil {
		return out
	}
	out["state"] = "observed_unverified"
	out["go_version"] = info.GoVersion
	out["module"] = info.Main.Path
	out["path"] = info.Path
	for _, entry := range info.Settings {
		switch entry.Key {
		case "vcs", "vcs.revision", "vcs.modified":
			out[entry.Key] = entry.Value
		}
	}
	return out
}
func validateBuild(info *debug.BuildInfo, head string) error {
	if info == nil {
		return errors.New("actual Go build provenance missing; rebuild from committed source with VCS stamping")
	}
	evidence := buildEvidence(info)
	if info.GoVersion != "go1.26.8" || info.Main.Path != "github.com/zerodenet/zboard/backend" || info.Path != "github.com/zerodenet/zboard/backend/tools/acceptance-principal-statistics" || evidence["vcs"] != "git" || evidence["vcs.revision"] != head || evidence["vcs.modified"] != "false" {
		return errors.New("binary does not prove this exact clean source HEAD/module; rebuild with VCS stamping")
	}
	return nil
}
