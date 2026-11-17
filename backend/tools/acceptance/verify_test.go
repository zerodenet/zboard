package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestVerifierDistinguishesSettledQuotaFromPendingStatistics(t *testing.T) {
	dir := t.TempDir()
	if err := seed(dir, 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	var f fixture
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "expected.json"), map[string]any{"run_id": f.RunID, "expected_bytes": map[uint]int64{}}); err != nil {
		t.Fatal(err)
	}
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(dir, "zboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Create(&model.MeteringUsageProjectionPending{TrafficRecordID: 1, ProtocolEndpointID: 1, CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := verify(dir); err == nil {
		t.Fatal("settled quota concealed undrained statistics")
	}
	data, err = os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		AccountingPassed bool `json:"accounting_passed"`
		StatisticsPassed bool `json:"statistics_passed"`
		Pending          int  `json:"pending_usage_projections"`
	}
	if err := json.Unmarshal(data, &result); err != nil || !result.AccountingPassed || result.StatisticsPassed || result.Pending != 1 {
		t.Fatal("settlement and projection milestones were conflated", result, err)
	}
	if n, err := (meteringstore.UsageProjectionWorker{DB: db}).Drain(t.Context(), 1); err != nil || n != 1 {
		t.Fatal("recover projection", n, err)
	}
	if err := verify(dir); err != nil {
		t.Fatal("fully recovered run rejected", err)
	}
}

func TestVerifierRejectsUnbalancedAndOverchargedLedger(t *testing.T) {
	dir := t.TempDir()
	if err := seed(dir, 2, 4, 17); err != nil {
		t.Fatal(err)
	}
	var f fixture
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "expected.json"), map[string]any{"run_id": f.RunID, "expected_bytes": map[uint]int64{}}); err != nil {
		t.Fatal(err)
	}
	if err := verify(dir); err != nil {
		t.Fatal(err)
	}
	if err := seed(dir, 2, 4, 17); err == nil {
		t.Fatal("seeder overwrote an existing fixture")
	}
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(dir, "zboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatalf("server startup after fixture migration: %v", err)
	}
	if err := db.Exec("UPDATE subscriptions SET flow_used=flow_used+1 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := verify(dir); err == nil {
		t.Fatal("accepted a balance that disagrees with both ledger and expected usage")
	}
	if err := db.Exec("UPDATE traffic_records SET used_bytes=used_bytes+1 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := verify(dir); err == nil {
		t.Fatal("accepted matching balance and ledger that both exceed expected usage")
	}
}

func TestSpoolVerifierDetectsPendingEventsAfterRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = filepath.Join(dir, "events")
	cfg.Storage.MinFreeSpace = 0
	cfg.Storage.EmergencyReserve = 0
	cfg.Compression.Enabled = false
	cfg.Compression.Algorithm = zeroevent.CompressionNone
	open := func() zeroevent.EventSpool {
		t.Helper()
		s, err := zeroevent.NewFileSpool(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	if err := s.Append(context.Background(), zeroevent.Envelope{ID: "pending", NodeID: 1, Type: "flow.updated", OccurredAt: time.Now(), Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifySpool(dir); err == nil {
		t.Fatal("unconsumed durable event was reported as drained")
	}
	s = open()
	batch, err := s.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("verifier consumed or lost pending event: %v %v", batch.Events, err)
	}
	if err := s.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifySpool(dir); err != nil {
		t.Fatal(err)
	}
}
