package meteringstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type cohortSQL struct {
	logger.Interface
	usageReads, writes int
	receiptWrites      int
	receiptRows        int64
	affected           int64
}

func (c *cohortSQL) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	text, rows := sql()
	upper := strings.ToUpper(text)
	if strings.HasPrefix(upper, "SELECT") && strings.Contains(text, "flow_usages") {
		c.usageReads++
	}
	if strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") || strings.HasPrefix(upper, "DELETE") {
		c.writes++
		c.affected += rows
		if strings.HasPrefix(upper, "INSERT") && strings.Contains(text, "metering_report_receipts") {
			c.receiptWrites++
			c.receiptRows += rows
		}
	}
}
func cohortDB(t *testing.T) (*gorm.DB, model.ProtocolCredential) {
	t.Helper()
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "cohort.db"))
	if err := db.AutoMigrate(&model.User{}, &model.Node{}, &model.ProtocolEndpoint{}, &model.Subscription{}, &model.ProtocolCredential{}, &model.FlowUsage{}, &model.ClosedEntitlement{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"ALTER TABLE subscriptions ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE protocol_credentials ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE protocol_credentials ADD COLUMN current_scope INTEGER DEFAULT 1",
		"ALTER TABLE protocol_credentials ADD COLUMN delivery_required INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE protocol_credentials ADD COLUMN principal_digest TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE protocol_credentials ADD COLUMN secret_principal_digest TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE protocol_credentials ADD COLUMN principal_lookup_version INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE protocol_credentials ADD COLUMN retired_principal_hash TEXT NOT NULL DEFAULT ''",
		"CREATE INDEX idx_credential_lookup_backfill ON protocol_credentials(node_id,principal_lookup_version,id)",
		"CREATE INDEX idx_credential_principal_digest ON protocol_credentials(node_id,principal_digest)",
		"CREATE INDEX idx_credential_secret_digest ON protocol_credentials(node_id,secret_principal_digest)",
		"CREATE INDEX idx_credential_retired_principal ON protocol_credentials(node_id,retired_principal_hash)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []any{&model.User{ID: 1, Email: "cohort@test"}, &model.Node{ID: 1}, &model.ProtocolEndpoint{ID: 1, MultiplierMilli: 1000}, &model.Subscription{ID: 1, UserID: 1, FlowTotal: 100000, Status: "active"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	cred := model.ProtocolCredential{ID: 1, UserID: 1, NodeID: 1, SubscriptionID: 1, ProtocolEndpointID: 1, CredentialID: "cohort-credential", PrincipalKey: "cohort-principal", PrincipalLookupVersion: 1}
	if err := db.Create(&cred).Error; err != nil {
		t.Fatal(err)
	}
	return db, cred
}
func cohortSample(c model.ProtocolCredential, flow, core string, seq uint64, up int64) metering.FlowSample {
	return metering.FlowSample{NodeID: c.NodeID, EventID: fmt.Sprintf("%s-%s-%d", core, flow, seq), EventType: "flow.updated", CoreInstanceID: core, FlowID: flow, PrincipalKey: c.PrincipalKey, Sequence: seq, Revision: seq, BytesUp: up, OccurredAt: time.Now().UTC()}
}
func applyCohort(db *gorm.DB, samples []metering.FlowSample) error {
	return db.Transaction(func(tx *gorm.DB) error {
		_, err := (BatchAccounting{EnqueuePublications: func(*gorm.DB, uint, uint) error { return nil }}).Apply(tx, samples)
		return err
	})
}
func cohortTotal(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var sub model.Subscription
	if err := db.First(&sub, 1).Error; err != nil {
		t.Fatal(err)
	}
	var sum int64
	if err := db.Model(&model.TrafficRecord{}).Select("COALESCE(SUM(used_bytes),0)").Scan(&sum).Error; err != nil {
		t.Fatal(err)
	}
	if sub.FlowUsed != want || sum != want {
		t.Fatalf("sub=%d ledger=%d want=%d", sub.FlowUsed, sum, want)
	}
}
func TestSQLiteCursorCohort32ReadsOnceWithoutAdditionalWrites(t *testing.T) {
	db, cred := cohortDB(t)
	for batch := 0; batch < 2; batch++ {
		samples := make([]metering.FlowSample, 32)
		for i := range samples {
			samples[i] = cohortSample(cred, fmt.Sprint(i), "core", uint64(batch+1), int64((batch+1)*30))
		}

		baseline := &cohortSQL{Interface: logger.Discard}
		if err := db.Session(&gorm.Session{Logger: baseline}).Transaction(func(tx *gorm.DB) error {
			for _, sample := range samples {
				if _, _, _, err := LoadFlowUsage(tx, sample.NodeID, sample.CoreInstanceID, sample.FlowID, cred.ID, sample.Revision, metering.FlowCounters{Raw: sample.BytesUp, Upload: sample.BytesUp}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if baseline.usageReads != 32 || baseline.writes != 0 {
			t.Fatalf("individual control reads=%d writes=%d", baseline.usageReads, baseline.writes)
		}
		count := &cohortSQL{Interface: logger.Discard}
		if err := applyCohort(db.Session(&gorm.Session{Logger: count}), samples); err != nil {
			t.Fatal(err)
		}
		// Keep the original cohort's 37 writes/99 affected rows exactly. The
		// separate identity authority adds one bounded insert of 32 receipts.
		if count.usageReads != 1 || count.writes-count.receiptWrites != 37 || count.affected-count.receiptRows != 99 || count.receiptWrites != 1 || count.receiptRows != 32 {
			t.Fatalf("batch%d reads=%d original-writes=%d original-rows=%d receipt-writes=%d receipt-rows=%d want1/37/99/1/32", batch, count.usageReads, count.writes-count.receiptWrites, count.affected-count.receiptRows, count.receiptWrites, count.receiptRows)
		}
		cohortTotal(t, db, int64(batch+1)*960)
	}
}
func TestSQLiteCursorCohortKeepsUnmergedSampleOrderAndReplay(t *testing.T) {
	db, c := cohortDB(t)
	a := cohortSample(c, "f", "core", 1, 1)
	b := cohortSample(c, "f", "core", 2, 2)
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id=1").Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	if err := applyCohort(db, []metering.FlowSample{a, b, b}); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 4)
	if err := applyCohort(db, []metering.FlowSample{a, b}); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 4)
	var row model.FlowUsage
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Revision != 2 || row.RawBytes != 2 || row.UsedBytes != 4 {
		t.Fatal(row)
	}
}
func TestSQLiteCursorCohortMigratesLegacyOnceAcrossTwoRuntimes(t *testing.T) {
	db, c := cohortDB(t)
	old := model.FlowUsage{NodeID: 1, FlowID: "f", ProtocolCredentialID: 1, SubscriptionID: 1, ProtocolEndpointID: 1, Status: "active", Revision: 1, RawBytes: 10, UploadBytes: 10, UsedBytes: 10}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := applyCohort(db, []metering.FlowSample{cohortSample(c, "f", "a", 2, 20), cohortSample(c, "f", "b", 3, 30)}); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 40)
	var rows []model.FlowUsage
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != old.ID || rows[0].FlowID != metering.FlowUsageKey("a", "f") || rows[0].UsedBytes != 20 || rows[1].FlowID != metering.FlowUsageKey("b", "f") || rows[1].UsedBytes != 30 {
		t.Fatal(rows)
	}
}
func TestSQLiteCursorCohortPreservesCompletedStaleAndInvalidBoundaries(t *testing.T) {
	for _, kind := range []string{"completed", "stale", "regression", "principal", "legacy-incompatible"} {
		t.Run(kind, func(t *testing.T) {
			db, c := cohortDB(t)
			row := model.FlowUsage{NodeID: 1, FlowID: metering.FlowUsageKey("core", "f"), ProtocolCredentialID: 1, SubscriptionID: 1, Status: "active", Revision: 5, RawBytes: 10, UploadBytes: 10}
			sample := cohortSample(c, "f", "core", 6, 20)
			switch kind {
			case "completed":
				row.Status = "completed"
			case "stale":
				sample.Sequence = 5
			case "regression":
				sample.BytesUp = 9
			case "principal":
				row.ProtocolCredentialID = 2
			case "legacy-incompatible":
				row.FlowID = "f"
				row.ProtocolCredentialID = 2
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			err := applyCohort(db, []metering.FlowSample{sample})
			if (kind == "regression" || kind == "principal") != (err != nil) {
				t.Fatalf("%s error=%v", kind, err)
			}
			want := int64(0)
			if kind == "legacy-incompatible" {
				want = 20
			}
			cohortTotal(t, db, want)
		})
	}
}
func TestSQLiteCursorCohortExhaustionAndClosedEpochIsolation(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprint(closed), func(t *testing.T) {
			db, c := cohortDB(t)
			if err := db.Model(&model.Subscription{}).Where("id=1").Updates(map[string]any{"flow_total": 3, "entitlement_epoch": 1}).Error; err != nil {
				t.Fatal(err)
			}
			if closed {
				if err := db.Create(&model.ClosedEntitlement{SubscriptionID: 1, Epoch: 0, UserID: 1, FlowTotal: 3}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				c.EntitlementEpoch = 1
				if err := db.Model(&model.ProtocolCredential{}).Where("id=1").Update("entitlement_epoch", 1).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := applyCohort(db, []metering.FlowSample{cohortSample(c, "f", "core", 1, 2), cohortSample(c, "f", "core", 2, 4)}); err != nil {
				t.Fatal(err)
			}
			var sub model.Subscription
			var cred model.ProtocolCredential
			var sum int64
			db.First(&sub, 1)
			db.First(&cred, 1)
			db.Model(&model.TrafficRecord{}).Select("SUM(used_bytes)").Scan(&sum)
			if sum != 3 {
				t.Fatal(sum)
			}
			if closed {
				var row model.ClosedEntitlement
				db.First(&row)
				if sub.FlowUsed != 0 || row.FlowUsed != 3 || cred.Status != "active" {
					t.Fatal(sub, row, cred)
				}
			} else if sub.FlowUsed != 3 || cred.Status != "expired" {
				t.Fatal(sub, cred)
			}
		})
	}
}
func TestSQLiteCursorCohortLateFailureRollsBackAndReplays(t *testing.T) {
	db, c := cohortDB(t)
	samples := []metering.FlowSample{cohortSample(c, "f", "core", 1, 2), cohortSample(c, "f", "core", 2, 4)}
	if err := db.Exec("CREATE TRIGGER reject_cohort_touch BEFORE UPDATE ON protocol_credentials WHEN NEW.last_used_at IS NOT NULL BEGIN SELECT RAISE(ABORT,'touch rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := applyCohort(db, samples); err == nil {
		t.Fatal("failure ignored")
	}
	cohortTotal(t, db, 0)
	for _, table := range []string{"flow_usages", "traffic_records", "metering_usage_projection_pending"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	if err := db.Exec("DROP TRIGGER reject_cohort_touch").Error; err != nil {
		t.Fatal(err)
	}
	if err := applyCohort(db, samples); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 4)
	if err := applyCohort(db, samples); err != nil {
		t.Fatal(err)
	}
	cohortTotal(t, db, 4)
}

func TestSQLiteCursorCohortPreloadsAfterCredentialLockAndBoundsKeys(t *testing.T) {
	db, c := cohortDB(t)
	credentialLocked := false
	if err := db.Callback().Query().After("gorm:query").Register("test:credential_before_cursor", func(tx *gorm.DB) {
		if tx.Statement.Table == "protocol_credentials" {
			if _, ok := tx.Statement.Clauses["FOR"]; ok {
				credentialLocked = true
			}
		}
		if tx.Statement.Table == "flow_usages" {
			if !credentialLocked {
				t.Error("cursor cohort read preceded credential/ownership locks")
			}
			if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
				t.Error("cursor cohort escaped authority transaction")
			}
			if len(tx.Statement.Vars) > 201 {
				t.Errorf("unbounded cursor SQL params=%d", len(tx.Statement.Vars))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	samples := make([]metering.FlowSample, 205)
	for i := range samples {
		samples[i] = cohortSample(c, fmt.Sprint(i), "core", 1, 1)
	}
	count := &cohortSQL{Interface: logger.Discard}
	if err := applyCohort(db.Session(&gorm.Session{Logger: count}), samples); err != nil {
		t.Fatal(err)
	}
	if count.usageReads != 3 {
		t.Fatalf("410 keys require three <=200-key reads, got %d", count.usageReads)
	}
	cohortTotal(t, db, 205)
}

func TestSQLiteCursorCohortDisabledPathKeepsIndividualReads(t *testing.T) {
	db, c := cohortDB(t)
	count := &cohortSQL{Interface: logger.Discard}
	if err := db.Session(&gorm.Session{Logger: count}).Transaction(func(tx *gorm.DB) error {
		batch := newFlowBatch(tx)
		// Exercise the exact branch selected by the production MySQL dialect;
		// no MySQL server or collation emulation is involved in this test.
		batch.batchReports = false
		sample := cohortSample(c, "f", "core", 1, 1)
		for i := 0; i < 2; i++ {
			if _, _, _, err := batch.loadUsage(tx, sample, c.ID, metering.FlowCounters{Raw: 1, Upload: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count.usageReads != 2 || count.writes != 0 {
		t.Fatal(count)
	}
}

func TestDeletedProtocolKeepsLateAccountingAndReplayOwnership(t *testing.T) {
	db, cred := cohortDB(t)
	first := cohortSample(cred, "historical-flow", "core", 1, 50)
	if err := applyCohort(db, []metering.FlowSample{first}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", cred.ProtocolEndpointID).Updates(map[string]any{"is_active": false, "deleted_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProtocolCredential{}).Where("id = ?", cred.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	late := cohortSample(cred, "historical-flow", "core", 2, 80)
	for range 2 {
		if err := applyCohort(db, []metering.FlowSample{late}); err != nil {
			t.Fatal("historical ownership was hidden by current deletion", err)
		}
	}
	cohortTotal(t, db, 80)
	var usage model.FlowUsage
	if err := db.First(&usage).Error; err != nil || usage.ProtocolEndpointID != cred.ProtocolEndpointID || usage.RawBytes != 80 {
		t.Fatal(usage, err)
	}
	var live int64
	db.Model(&model.ProtocolEndpoint{}).Where("id = ?", cred.ProtocolEndpointID).Count(&live)
	if live != 0 {
		t.Fatal("late accounting restored current protocol")
	}
}
