package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestReportReceiptBackfillOrphanDefaultsCannotStartOrdinaryServices(t *testing.T) {
	for _, option := range []string{"backfill-report-receipts-after", "backfill-report-receipts-through", "backfill-report-receipts-batch"} {
		flags := flag.NewFlagSet("backfill", flag.ContinueOnError)
		value := "0"
		if option == "backfill-report-receipts-batch" {
			value = "200"
		}
		flags.String(option, value, "test option")
		if err := flags.Parse([]string{"-" + option + "=" + value}); err != nil {
			t.Fatal(err)
		}
		if err := validateReportBackfillFlags(false, flags); err == nil {
			t.Fatal("explicit orphan default was mistaken for ordinary startup", option)
		}
		if err := validateReportBackfillFlags(true, flags); err != nil {
			t.Fatal("explicit maintenance mode rejected its own range", err)
		}
	}
}

func TestReportReceiptBackfillModeExclusionAndBounds(t *testing.T) {
	for bits := range 16 {
		if err := validateReportBackfillMode(true, bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0, 0, 10, 200); (err != nil) != (bits != 0) {
			t.Fatal("incompatible mode admitted", bits, err)
		}
	}
	for _, v := range []struct {
		after, through uint
		batch          int
	}{{0, 0, 200}, {10, 10, 200}, {11, 10, 200}, {0, 10, 0}, {0, 10, 201}} {
		if err := validateReportBackfillMode(true, false, false, false, false, v.after, v.through, v.batch); err == nil {
			t.Fatal("unbounded execution admitted", v)
		}
	}
	if err := validateReportBackfillMode(false, false, false, false, false, 0, 10, 200); err == nil {
		t.Fatal("orphan range admitted")
	}
}

func TestReportReceiptBackfillMissingSQLiteDoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	c := projectionWorkerTestConfig(filepath.Join(dir, "missing.db"), dir)
	if _, err := runReportReceiptBackfill(t.Context(), c, 0, 10, 1); err == nil {
		t.Fatal("missing source admitted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("inspection created files", files, err)
	}
}

func TestReportReceiptBackfillEntrypoint(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_RECEIPT_BACKFILL_CHILD") == "1" {
		main()
		os.Exit(0)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "prepared.db")
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := application.PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	fact := model.TrafficRecord{NodeID: 7, ProtocolEndpointID: 9, ReportID: "old-command-fact", Nonce: "old-command-nonce", RawBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: time.Now().UTC(), Meta: "historical body must remain"}
	if err := db.Create(&fact).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := projectionWorkerTestConfig(path, dir)
	c.Port = listener.Addr().(*net.TCPAddr).Port
	configPath := filepath.Join(dir, "backfill.json")
	raw, err := json.Marshal(map[string]any{"Name": c.Name, "Host": c.Host, "Port": c.Port, "Log": map[string]any{"Level": "error", "Mode": "console"}, "environment": c.Environment, "database_driver": c.DatabaseDriver, "datasource": c.DataSource, "jwt_secret": c.JwtSecret, "credential_encryption_key": c.CredentialEncryptionKey, "bootstrap_admin_email": c.BootstrapAdminEmail, "bootstrap_admin_password": c.BootstrapAdminPassword, "zero_event_spool_dir": c.ZeroEventSpoolDir, "file_storage_dir": c.FileStorageDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	tables := []string{"users", "system_configs", "job_runs", "job_schedules", "metering_usage_projection_pending"}
	before := map[string]int64{}
	for _, table := range tables {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		before[table] = count
	}
	run := func() metering.ReportReceiptBackfillResult {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReportReceiptBackfillEntrypoint$", "-f", configPath, "-backfill-report-receipts", fmt.Sprintf("-backfill-report-receipts-through=%d", fact.ID), "-backfill-report-receipts-batch=1")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "ZBOARD_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "ZBOARD_TEST_RECEIPT_BACKFILL_CHILD=1")
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("actual bounded entrypoint failed: %v", err)
		}
		var result metering.ReportReceiptBackfillResult
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("invalid command receipt: %v %s", err, output)
		}
		return result
	}
	first := run()
	second := run()
	if first != second || first.Scanned != 1 || first.IdentitiesVerified != 1 || !first.RangeExhausted {
		t.Fatal("new process replay changed result", first, second)
	}
	var receipts int64
	if err := db.Model(&model.MeteringReportReceipt{}).Count(&receipts).Error; err != nil || receipts != 1 {
		t.Fatal("identity missing or duplicated", receipts, err)
	}
	var current model.TrafficRecord
	if err := db.First(&current, fact.ID).Error; err != nil || current.Meta != fact.Meta || current.UsedBytes != 5 || current.RawBytes != 3 || current.Nonce != fact.Nonce {
		t.Fatal("command changed financial evidence", err)
	}
	for _, table := range tables {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != before[table] {
			t.Fatal("backfill started ordinary services or bootstrap", table, count, err)
		}
	}
	if _, err := os.Stat(c.ZeroEventSpoolDir); !os.IsNotExist(err) {
		t.Fatal("backfill opened event spool", err)
	}
}
