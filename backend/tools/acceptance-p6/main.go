//go:build linux

// acceptance-p6 is an isolated manual experiment tool, never linked into zboard.
package main

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/localworkers"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zerodenet/zboard/backend/internal/version"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

var mode = flag.String("mode", "observe", "prepare, observe, slowwrite, or stop-observer")
var directory = flag.String("dir", "", "fresh isolated acceptance volume")
var owner = flag.String("owner", "", "runner-generated ownership token")
var runID = flag.String("run-id", "", "exact fixture run_id")
var backlog = flag.Bool("backlog", false, "enqueue each existing seeded historical ledger once, before server startup")
var seconds = flag.Int("seconds", 60, "bounded experiment duration, at most 900")

type ownership struct {
	Owner string `json:"owner"`
	RunID string `json:"run_id"`
}

func emit(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }
func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, out)
	}
	return err
}
func checkOwner(preparing bool) error {
	if !filepath.IsAbs(*directory) || len(*owner) != 32 || strings.Trim(*owner, "0123456789abcdef") != "" || *runID == "" {
		return errors.New("explicit absolute fixture directory/owner/run_id required")
	}
	var fixture struct {
		RunID string `json:"run_id"`
	}
	if err := readJSON(filepath.Join(*directory, "fixture.json"), &fixture); err != nil || fixture.RunID != *runID {
		return errors.New("fixture ownership/run_id differs")
	}
	path := filepath.Join(*directory, "p6-owner.json")
	if preparing {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		return json.NewEncoder(file).Encode(ownership{*owner, *runID})
	}
	var marker ownership
	if err := readJSON(path, &marker); err != nil || marker != (ownership{*owner, *runID}) {
		return errors.New("owned fixture marker differs; no operation admitted")
	}
	return nil
}
func database(readonly bool) (*sql.DB, error) {
	path := filepath.Join(*directory, "zboard.db")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("isolated SQLite file missing or symlink")
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	if readonly {
		dsn += "?mode=ro"
	}
	db, err := sql.Open("sqlite", dsn)
	if err == nil {
		db.SetMaxOpenConns(1)
		if readonly {
			_, err = db.Exec("PRAGMA query_only=ON")
		}
	}
	return db, err
}
func prepare() error {
	db, err := database(false)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE acceptance_p6_write_lock (id INTEGER PRIMARY KEY, revision INTEGER NOT NULL); INSERT INTO acceptance_p6_write_lock VALUES(1,0)"); err != nil {
		return err
	}
	if *backlog {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM metering_usage_projection_pending").Scan(&n); err != nil || n != 0 {
			return errors.New("preexisting projection intents; refusing duplicate replay")
		}
		// These are real, previously unprojected seed facts. Intent creation uses
		// the real current clock, never an invented old age or CPU observation.
		_, err = db.Exec(`INSERT INTO metering_usage_projection_pending(traffic_record_id,protocol_endpoint_id,created_at,hourly_pending,user_id,subscription_id,node_id,record_at)
SELECT id,protocol_endpoint_id,?,1,user_id,subscription_id,node_id,strftime('%Y-%m-%d %H:00:00',record_at) FROM traffic_records WHERE report_id LIKE 'history-%'`, time.Now().UTC().Format("2006-01-02 15:04:05.999999999-07:00"))
		if err != nil {
			return err
		}
	}
	// An ephemeral signer is trusted only by this new isolated configuration.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile("/p6busy")
	if err != nil {
		return err
	}
	manifest := plugins.Manifest{SchemaVersion: 1, ID: "acceptance.p6busy", Name: "Isolated P6 busy fixture", Version: "1.0.0", Requires: plugins.Requirements{ZBoard: "= " + strings.TrimPrefix(version.Version, "v"), Tested: []string{strings.TrimPrefix(version.Version, "v")}, Protocol: 1, Bridge: 1}, Capabilities: []string{plugins.ConfigCapability}, Files: map[string]string{"runtimes/native": digest(binary)}}
	manifest.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{"linux-amd64": "runtimes/native"}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	signature, _ := json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "acceptance.p6", PublicKey: base64.StdEncoding.EncodeToString(pub), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw))})
	file, err := os.OpenFile(filepath.Join(*directory, "p6busy.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for name, data := range map[string][]byte{"manifest.json": raw, "signature.json": signature, "runtimes/native": binary} {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	var configuration map[string]any
	if err := readJSON(filepath.Join(*directory, "zboard.json"), &configuration); err != nil {
		return err
	}
	configuration["plugins"] = map[string]any{"directory": "/data/plugins", "trusted_publishers": map[string]string{"acceptance.p6": base64.StdEncoding.EncodeToString(pub)}}
	encoded, _ := json.MarshalIndent(configuration, "", "  ")
	if err := os.WriteFile(filepath.Join(*directory, "zboard.json"), encoded, 0600); err != nil {
		return err
	}
	emit(map[string]any{"prepared": true, "owner": *owner, "run_id": *runID, "backlog_seeded": *backlog, "native_sha256": digest(binary), "manifest_sha256": digest(raw), "signing_fingerprint": digest(pub)})
	return nil
}
func observe() error {
	db, err := database(true)
	if err != nil {
		return err
	}
	defer db.Close()
	sampler := localworkers.HostResources{StoragePaths: []string{*directory, filepath.Join(*directory, "events")}}
	deadline := time.Now().Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(*directory, "p6-observer.stop")); err == nil {
			return nil
		}
		now := time.Now().UTC()
		row := map[string]any{"at": now, "resources": sampler.Sample(now), "sql": map[string]any{}}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		queries := map[string]string{"usage_pending": "SELECT COUNT(*) FROM metering_usage_projection_pending", "node_observation_pending": "SELECT COUNT(*) FROM node_observation_intents WHERE status IN ('pending','claimed')", "settled_flow_used": "SELECT COALESCE(SUM(flow_used),0) FROM subscriptions", "exhausted_subscriptions": "SELECT COUNT(*) FROM subscriptions WHERE flow_total>0 AND flow_used>=flow_total", "observed_excess_bytes": "SELECT COALESCE(SUM(MAX(0,flow_used-flow_total)),0) FROM subscriptions WHERE flow_total>0 AND flow_used>=flow_total"}
		for key, query := range queries {
			start := time.Now()
			var n int64
			if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
				row["sql"].(map[string]any)[key] = map[string]any{"known": false, "error": err.Error()}
			} else {
				row["sql"].(map[string]any)[key] = map[string]any{"known": true, "value": n, "query_seconds": time.Since(start).Seconds()}
			}
		}
		cancel()
		values := row["sql"].(map[string]any)
		exhausted := values["exhausted_subscriptions"].(map[string]any)
		if exhausted["known"] != true || exhausted["value"] == int64(0) {
			values["observed_excess_bytes"] = map[string]any{"known": false, "status": "not_exercised_or_unknown", "reason": "no confirmed exhausted subject; zero is not a stop/overuse guarantee"}
		}
		emit(row)
		time.Sleep(2 * time.Second)
	}
	return nil
}
func slowwrite() error {
	db, err := database(false)
	if err != nil {
		return err
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	deadline := time.Now().Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(deadline) {
		started := time.Now()
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE acceptance_p6_write_lock SET revision=revision+1 WHERE id=1"); err != nil {
			_ = tx.Rollback()
			return err
		}
		locked := time.Now()
		time.Sleep(250 * time.Millisecond)
		err = tx.Commit()
		emit(map[string]any{"started_at": started.UTC(), "write_lock_at": locked.UTC(), "finished_at": time.Now().UTC(), "actual_acquire_seconds": locked.Sub(started).Seconds(), "held_seconds": time.Since(locked).Seconds(), "committed": err == nil})
		if err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// Crash only the sole managed child of the owned fixture's actual PID 1.
// Process identity is rechecked immediately before signaling; no lock is removed.
func crashWorker() error {
	parent, err := os.ReadFile("/proc/1/cmdline")
	if err != nil || !strings.Contains(string(parent), "/data/zboard.json") {
		return errors.New("PID1 is not the fixture server")
	}
	expected, err := os.Stat("/zboard")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		base := "/proc/" + entry.Name()
		args, err := os.ReadFile(base + "/cmdline")
		if err != nil || !strings.Contains(string(args), "-projection-worker-background") {
			continue
		}
		status, err := os.ReadFile(base + "/status")
		if err != nil || !strings.Contains(string(status), "PPid:\t1\n") {
			continue
		}
		actual, err := os.Stat(base + "/exe")
		if err != nil || !os.SameFile(expected, actual) {
			continue
		}
		before, err := os.ReadFile(base + "/stat")
		if err != nil {
			continue
		}
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			return fmt.Errorf("pidfd unavailable; no crash executed: %w", err)
		}
		defer unix.Close(fd)
		after, err := os.ReadFile(base + "/stat")
		if err != nil {
			return err
		}
		beforeFields := strings.Fields(string(before))
		afterFields := strings.Fields(string(after))
		if len(beforeFields) < 22 || len(afterFields) < 22 || beforeFields[21] != afterFields[21] {
			return errors.New("worker identity changed before signal; no crash admitted")
		}
		err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		emit(map[string]any{"at": time.Now().UTC(), "worker_pid": pid, "signaled": err == nil, "owned_parent": 1, "executable_same_as_server": true, "pidfd_identity": true})
		return err
	}
	return errors.New("no owned managed worker is running; crash not executed")
}

func main() {
	flag.Parse()
	if *seconds < 1 || *seconds > 900 {
		fmt.Fprintln(os.Stderr, "duration must be 1-900 seconds")
		os.Exit(1)
	}
	err := checkOwner(*mode == "prepare")
	if err == nil {
		switch *mode {
		case "prepare":
			err = prepare()
		case "observe":
			err = observe()
		case "slowwrite":
			err = slowwrite()
		case "crash-worker":
			err = crashWorker()
		case "stop-observer":
			err = os.WriteFile(filepath.Join(*directory, "p6-observer.stop"), []byte(*owner), 0600)
		default:
			err = errors.New("unknown mode")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
