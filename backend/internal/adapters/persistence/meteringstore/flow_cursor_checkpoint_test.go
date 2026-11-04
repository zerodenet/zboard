package meteringstore

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCompletedFlowCompactionRetainsArbitrarilyLateAccountingFloor(t *testing.T) {
	db, cred := cohortDB(t)
	// Completion resolves historical identity through its canonical digest index.
	if err := db.Model(&cred).Updates(map[string]any{"principal_lookup_version": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id=1").Update("multiplier_milli", 1001).Error; err != nil {
		t.Fatal(err)
	}
	store := CompletionAccounting{DB: db, EnqueuePublications: func(*gorm.DB, uint, uint) error { return nil }}
	event := metering.CompletedFlow{NodeID: 1, CoreInstanceID: "old-runtime", FlowID: "connection", EventID: "first", PrincipalKey: cred.PrincipalKey, EventType: "flow.completed", Sequence: 3, Revision: 3, BytesUp: 100, OccurredAt: time.Now().UTC().AddDate(-20, 0, 0)}
	if _, _, err := store.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 101)
	now := time.Now().UTC().Add(48 * time.Hour)
	compact := FlowCursorCompaction{DB: db, Now: func() time.Time { return now }}
	if n, err := compact.Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n := statisticsCount(t, db, "flow_usages"); n != 0 {
		t.Fatal("completed hot records retained", n)
	}
	if n := statisticsCount(t, db, "flow_cursor_checkpoints"); n != 1 {
		t.Fatal("accounting floor missing", n)
	}
	// A new report identity, twenty-year-old event time, larger counters: only
	// the delta may be charged. An ordinary replay still uses the old receipt.
	event.EventID, event.Sequence, event.Revision, event.BytesUp = "late", 4, 4, 130
	if _, _, err := store.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 132)
	if _, _, err := store.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 132)
	if n := statisticsCount(t, db, "flow_cursor_checkpoints"); n != 0 {
		t.Fatal("hot/cold authority duplicated", n)
	}
	// Compact again after the renewed hot horizon, then reject regression and
	// cross-principal changes without modifying either quota or the checkpoint.
	compact.Now = func() time.Time { return now.Add(48 * time.Hour) }
	if _, err := compact.Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	event.EventID, event.Sequence, event.Revision, event.BytesUp = "regression", 2, 2, 90
	if _, _, err := store.Complete(t.Context(), event); err == nil {
		t.Fatal("regressing old completion accepted")
	}
	cohortTotal(t, db, 132)
	stale := cohortSample(cred, "connection", "old-runtime", 99, 150)
	if err := applyCohort(db, []metering.FlowSample{stale}); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 132) // completed flows cannot reopen through updated events
}

func TestCompletedFloorCollationAliasCannotCreateSecondHotIdentity(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "identity-collation.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	// Match a case-insensitive source identity, without relying on Go equality.
	for _, ddl := range []string{
		"CREATE TABLE flow_usages (id INTEGER PRIMARY KEY, node_id INTEGER NOT NULL,flow_id TEXT COLLATE NOCASE NOT NULL,UNIQUE(node_id,flow_id))",
		"CREATE TABLE flow_cursor_checkpoints (node_id INTEGER NOT NULL,flow_id TEXT COLLATE NOCASE NOT NULL,protocol_credential_id INTEGER NOT NULL,revision INTEGER NOT NULL,raw_bytes INTEGER NOT NULL,upload_bytes INTEGER NOT NULL,download_bytes INTEGER NOT NULL,used_bytes INTEGER NOT NULL,is_completed NUMERIC NOT NULL,PRIMARY KEY(node_id,flow_id))",
		"CREATE TABLE debit_probe (amount INTEGER NOT NULL)", "INSERT INTO debit_probe VALUES(0)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	floor := model.FlowCursorCheckpoint{IsCompleted: true, NodeID: 1, FlowID: "Original", ProtocolCredentialID: 1, Revision: 4, RawBytes: 100, UsedBytes: 100}
	if err := db.Create(&floor).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		_, found, _, err := LoadFlowUsage(tx, 1, "", "ORIGINAL", 1, 5, metering.FlowCounters{Raw: 150})
		if err != nil {
			return err
		}
		if found {
			t.Fatal("changed exact identity unexpectedly accepted")
		}
		if err := tx.Exec("UPDATE debit_probe SET amount=50").Error; err != nil {
			return err
		}
		row := model.FlowUsage{NodeID: 1, FlowID: "ORIGINAL", ProtocolCredentialID: 1, RawBytes: 150}
		return saveFlowUsage(tx, &row, false, "")
	})
	if err == nil || !strings.Contains(err.Error(), "compact accounting floor") {
		t.Fatal("collation alias bypassed compact identity", err)
	}
	var amount int
	if err := db.Table("debit_probe").Select("amount").Scan(&amount).Error; err != nil || amount != 0 {
		t.Fatal("failed identity admission committed debit", amount, err)
	}
	// A legacy hot cursor may also rename into a runtime key. The cross-table
	// identity check must cover UPDATE as well as INSERT.
	key := metering.FlowUsageKey("core", "legacy")
	other := model.FlowCursorCheckpoint{IsCompleted: true, NodeID: 1, FlowID: strings.ToUpper(key), ProtocolCredentialID: 1, Revision: 9, RawBytes: 100, UsedBytes: 100}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO flow_usages(id,node_id,flow_id) VALUES(5,1,'legacy')").Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE debit_probe SET amount=50").Error; err != nil {
			return err
		}
		row := model.FlowUsage{ID: 5, NodeID: 1, FlowID: key, ProtocolCredentialID: 1, RawBytes: 150}
		return saveFlowUsage(tx, &row, true, "legacy")
	})
	if err == nil || !strings.Contains(err.Error(), "compact accounting floor") {
		t.Fatal("legacy rename bypassed compact identity", err)
	}
	if err := db.Table("debit_probe").Select("amount").Scan(&amount).Error; err != nil || amount != 0 {
		t.Fatal("legacy rename committed debit", amount, err)
	}
}

func TestFlowCompactionIsBoundedAtomicAndRevisitsSkippedRows(t *testing.T) {
	db, _ := cohortDB(t)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	for i := 1; i <= 5; i++ {
		row := model.FlowUsage{NodeID: 1, FlowID: fmt.Sprint(i), ProtocolCredentialID: 1, Revision: 9, RawBytes: 20, UploadBytes: 20, UsedBytes: 20, Status: "completed", CompletedAt: &old}
		if i == 1 {
			row.Status = "active"
			row.CompletedAt = nil
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	s := FlowCursorCompaction{DB: db, Now: func() time.Time { return now }}
	fault := errors.New("injected deletion failure")
	db.Callback().Delete().Before("gorm:delete").Register("test:compaction_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "flow_usages" {
			tx.AddError(fault)
		}
	})
	if _, err := s.Drain(t.Context(), 2); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	db.Callback().Delete().Remove("test:compaction_failure")
	if statisticsCount(t, db, "flow_usages") != 5 || statisticsCount(t, db, "flow_cursor_checkpoints") != 0 || statisticsCount(t, db, "flow_cursor_compaction_states") != 0 {
		t.Fatal("failed transfer committed partially")
	}
	if n, err := s.Drain(t.Context(), 2); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if statisticsCount(t, db, "flow_usages") != 4 {
		t.Fatal("scan budget did not include active rows")
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Drain(t.Context(), 2); err != nil {
			t.Fatal(err)
		}
	}
	if ready, err := s.HasPending(t.Context()); err != nil || ready {
		t.Fatal("completed pass did not sleep", ready, err)
	}
	if err := db.Model(&model.FlowUsage{}).Where("flow_id = ?", "1").Updates(map[string]any{"status": "completed", "completed_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if n, err := s.Drain(t.Context(), 2); err != nil || n != 1 {
		t.Fatal("skipped connection was not revisited", n, err)
	}
	if statisticsCount(t, db, "flow_cursor_checkpoints") != 5 {
		t.Fatal("missing compacted floor")
	}
}

func TestAbandonedActiveCheckpointContinuesAfterArbitrarilyLateUpdate(t *testing.T) {
	for _, firstCore := range []string{"old-runtime", ""} {
		t.Run("core="+firstCore, func(t *testing.T) {
			db, cred := cohortDB(t)
			if err := db.Model(&model.ProtocolEndpoint{}).Where("id=1").Update("multiplier_milli", 1001).Error; err != nil {
				t.Fatal(err)
			}
			first := cohortSample(cred, "abandoned", firstCore, 1, 100)
			if err := applyCohort(db, []metering.FlowSample{first}); err != nil {
				t.Fatal(err)
			}
			cohortTotal(t, db, 101)
			old := time.Now().UTC().AddDate(-20, 0, 0)
			if err := db.Model(&model.FlowUsage{}).Where("node_id=1").UpdateColumns(map[string]any{"updated_at": old, "last_seen_at": old}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := (FlowCursorCompaction{DB: db}).Drain(t.Context(), 200); err != nil {
				t.Fatal(err)
			}
			var floor model.FlowCursorCheckpoint
			if err := db.First(&floor).Error; err != nil || floor.IsCompleted || floor.UsedBytes != 101 {
				t.Fatal("active floor became terminal or lost rounding", floor, err)
			}
			if statisticsCount(t, db, "flow_usages") != 0 {
				t.Fatal("missing completion retained full active history")
			}
			nextCore := firstCore
			if nextCore == "" {
				nextCore = "runtime-after-upgrade"
			}
			next := cohortSample(cred, "abandoned", nextCore, 2, 130)
			next.OccurredAt = old
			if err := applyCohort(db, []metering.FlowSample{next}); err != nil {
				t.Fatal("old active update/legacy continuation rejected", err)
			}
			cohortTotal(t, db, 132)
			if err := applyCohort(db, []metering.FlowSample{next}); err != nil {
				t.Fatal(err)
			}
			cohortTotal(t, db, 132)
			if statisticsCount(t, db, "flow_cursor_checkpoints") != 0 || statisticsCount(t, db, "flow_usages") != 1 {
				t.Fatal("active restore duplicated authority")
			}
		})
	}
}

func TestColdFlowWindowIntersectionAndIdentityConflictFailClosed(t *testing.T) {
	db, cred := cohortDB(t)
	key := metering.FlowUsageKey("core", "f")
	row := model.FlowCursorCheckpoint{IsCompleted: true, NodeID: 1, FlowID: key, ProtocolCredentialID: cred.ID, Revision: 10, RawBytes: 20, UploadBytes: 20, UsedBytes: 20}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MeteringReportReceipt{NodeID: 1, ReportID: "intersects", TrafficRecordID: 42, ProtocolEndpointID: 1, CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	sample := cohortSample(cred, "f", "core", 10, 20)
	sample.Checkpoints = []metering.FlowCheckpoint{{ReportID: "intersects"}}
	if err := db.Transaction(func(tx *gorm.DB) error { return validateFlowWindowReports(tx, []metering.FlowSample{sample}) }); err != nil {
		t.Fatal("compacted window cursor ignored", err)
	}
	sample.Sequence = 11
	if err := db.Transaction(func(tx *gorm.DB) error { return validateFlowWindowReports(tx, []metering.FlowSample{sample}) }); !errors.Is(err, metering.ErrFlowWindowNeedsIndividualReports) {
		t.Fatal(err)
	}
	hot := checkpointFlowUsage(row)
	if err := db.Create(&hot).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, _, _, err := LoadFlowUsage(tx, 1, "core", "f", cred.ID, 11, metering.FlowCounters{Raw: 30, Upload: 30})
		return err
	}); err == nil {
		t.Fatal("duplicate accounting authority accepted")
	}
}
