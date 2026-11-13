package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func retentionReports(t *testing.T) (*gorm.DB, CompletionAccounting, []metering.CompletedFlow, time.Time) {
	t.Helper()
	db, cred := cohortDB(t)
	if err := db.AutoMigrate(&model.TrafficDiagnosticMaterial{}, &model.TrafficMetaArchive{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&cred).Updates(map[string]any{"principal_lookup_version": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id=1").Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Hour).Add(-48 * time.Hour)
	clock := base
	db.Config.NowFunc = func() time.Time { return clock }
	store := CompletionAccounting{DB: db, EnqueuePublications: func(*gorm.DB, uint, uint) error { return nil }}
	reports := []metering.CompletedFlow{}
	for i, flow := range []string{"one", "two"} {
		clock = base.Add(time.Duration(i+1) * time.Minute)
		e := metering.CompletedFlow{NodeID: 1, CoreInstanceID: "retention-core", FlowID: flow, EventID: "report-" + flow, PrincipalKey: cred.PrincipalKey, EventType: "flow.completed", Sequence: uint64(i + 1), Revision: uint64(i + 1), BytesUp: 3, OccurredAt: clock}
		if _, _, err := store.Complete(t.Context(), e); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, e)
	}
	if _, err := (UsageProjectionWorker{DB: db}).Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	return db, store, reports, base
}
func TestLedgerRetentionMergeReplayLifetimeAndLateReport(t *testing.T) {
	db, accounting, reports, base := retentionReports(t)
	if got := resultTotal(t, db); got != 10 {
		t.Fatal(got)
	}
	now := base.Add(3 * 24 * time.Hour)
	retention := LedgerRetention{DB: db, Now: func() time.Time { return now }}
	if n, err := retention.Drain(t.Context(), 100); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	var rows []model.TrafficRecord
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].RetentionLevel != 1 || rows[0].UsedBytes != 10 || rows[0].RawBytes != 6 || rows[0].RecordCount != 2 {
		t.Fatal(rows, err)
	}
	if got := resultTotal(t, db); got != 10 {
		t.Fatal("hourly merge changed result", got)
	}
	for _, e := range reports {
		r, _, err := accounting.Complete(t.Context(), e)
		if err != nil || !r.IsAggregate || r.UsedBytes != 10 {
			t.Fatal(r, err)
		}
	}
	now = base.Add(32 * 24 * time.Hour)
	if n, err := retention.Drain(t.Context(), 100); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	rows = nil
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].RetentionLevel != 2 || rows[0].RecordCount != 2 {
		t.Fatal(rows, err)
	}
	now = base.Add(367 * 24 * time.Hour)
	if n, err := retention.Drain(t.Context(), 100); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var count int64
	db.Model(&model.TrafficRecord{}).Count(&count)
	if count != 0 {
		t.Fatal("expired detail not removed", count)
	}
	for _, e := range reports {
		r, _, err := accounting.Complete(t.Context(), e)
		if err != nil || r.RetentionLevel != 3 || r.UsedBytes != 10 {
			t.Fatal(r, err)
		}
	}
	if got := resultTotal(t, db); got != 10 {
		t.Fatal("lifetime transfer changed result", got)
	}
	db.Config.NowFunc = func() time.Time { return now }
	late := reports[0]
	late.EventID = "late-unseen"
	late.Sequence = 3
	late.Revision = 3
	late.BytesUp = 6
	if _, _, err := accounting.Complete(t.Context(), late); err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounting.Complete(t.Context(), late); err != nil {
		t.Fatal(err)
	}
	if got := resultTotal(t, db); got != 15 {
		t.Fatal("late report mischarged", got)
	}
	var sub model.Subscription
	if err := db.First(&sub, 1).Error; err != nil || sub.FlowUsed != 15 {
		t.Fatal(sub.FlowUsed, err)
	}
}
func TestLedgerRetentionFailureKeepsOriginalReceiptAndAmounts(t *testing.T) {
	db, accounting, reports, base := retentionReports(t)
	if err := db.Exec("CREATE TRIGGER reject_ledger_retirement BEFORE DELETE ON traffic_records BEGIN SELECT RAISE(ABORT,'delete failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	retention := LedgerRetention{DB: db, Now: func() time.Time { return base.Add(3 * 24 * time.Hour) }}
	if _, err := retention.Drain(t.Context(), 100); err == nil {
		t.Fatal("source deletion failure swallowed")
	}
	var count int64
	db.Model(&model.MeteringLedgerTarget{}).Count(&count)
	if count != 0 {
		t.Fatal("redirect survived failed source deletion", count)
	}
	db.Model(&model.TrafficRecord{}).Count(&count)
	if count != 2 {
		t.Fatal("failed retirement changed sources", count)
	}
	for _, e := range reports {
		if _, _, err := accounting.Complete(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	if got := resultTotal(t, db); got != 10 {
		t.Fatal(got)
	}
}
func TestLedgerRetentionPreservesExactAnniversaryResetBoundary(t *testing.T) {
	db, _ := cohortDB(t)
	if err := db.AutoMigrate(&model.TrafficDiagnosticMaterial{}, &model.TrafficMetaArchive{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 1, 5, 12, 34, 56, 123000000, time.UTC)
	cut := time.Date(2027, 1, 5, 12, 34, 56, 123000000, time.UTC)
	if err := db.Model(&model.Subscription{}).Where("id=1").Updates(map[string]any{"start_at": anchor, "end_at": now.AddDate(2, 0, 0), "reset_policy": 2, "next_reset_at": cut, "reset_quota_bytes": 100, "flow_used": 2, "flow_total": 100}).Error; err != nil {
		t.Fatal(err)
	}
	for i, offset := range []time.Duration{-time.Minute, time.Minute} {
		at := cut.Add(offset)
		db.Config.NowFunc = func() time.Time { return at }
		r := model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ProtocolEndpointID: 1, ReportID: []string{"before", "after"}[i], Nonce: []string{"nonce-before", "nonce-after"}[i], RawBytes: 1, UploadBytes: 1, UsedBytes: 1, ProtocolMultiplierMilli: 1000, At: at, CreatedAt: at, UpdatedAt: at}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
			return RecordUsageProjections(tx, []model.TrafficRecord{r})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (UsageProjectionWorker{DB: db}).Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	if n, err := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	var count int64
	db.Model(&model.TrafficRecord{}).Count(&count)
	if count != 2 {
		t.Fatal("straddling anniversary contributions merged", count)
	}
	var used int64
	if err := db.Model(&model.TrafficRecord{}).Select("COALESCE(SUM(used_bytes),0)").Where("subscription_id=1 AND entitlement_epoch=0 AND created_at>=? AND created_at<=?", cut, now).Scan(&used).Error; err != nil || used != 1 {
		t.Fatal("exact receive-time reset input changed", used, err)
	}

	var nonce model.MeteringNonceReceipt
	if err := db.Where("node_id=1 AND nonce=?", "nonce-before").First(&nonce).Error; err != nil || nonce.ReportID != "before" {
		t.Fatal(nonce, err)
	}
}

func TestLedgerRetentionHandlesUnattributedLegacyWithoutInventingReplayIdentity(t *testing.T) {
	db, _ := cohortDB(t)
	if e := db.AutoMigrate(&model.TrafficDiagnosticMaterial{}, &model.TrafficMetaArchive{}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	old := now.Add(-400 * 24 * time.Hour)
	row := model.TrafficRecord{RawBytes: 3, UsedBytes: 5, ReportID: "", Nonce: "orphan", At: old, CreatedAt: old, UpdatedAt: old}
	if e := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&row).Error; e != nil {
			return e
		}
		return addSettlementResults(tx, []model.TrafficRecord{row}, false)
	}); e != nil {
		t.Fatal(e)
	}
	if n, e := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if resultTotal(t, db) != 5 {
		t.Fatal("unattributed settled result lost")
	}
	var n int64
	db.Model(&model.TrafficRecord{}).Count(&n)
	if n != 0 {
		t.Fatal("orphan legacy detail retained", n)
	}
	db.Model(&model.MeteringLedgerTarget{}).Count(&n)
	if n != 0 {
		t.Fatal("unusable orphan redirect invented", n)
	}
}

func TestLedgerRetentionHoldsUnverifiedProofWithoutBlockingOtherSources(t *testing.T) {
	db, _, _, base := retentionReports(t)
	var first model.TrafficRecord
	if e := db.Order("id").First(&first).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&first).UpdateColumns(map[string]any{"meta": "{invalid", "updated_at": first.UpdatedAt}).Error; e != nil {
		t.Fatal(e)
	}
	now := base.Add(5 * 24 * time.Hour)
	n, e := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100)
	if n != 1 || e == nil {
		t.Fatal(n, e)
	}
	var state model.MeteringLedgerRetentionProgress
	if e := db.First(&state, 1).Error; e != nil || state.AfterID != state.UpperID || state.HeldCount != 1 || state.LastHeldID != first.ID {
		t.Fatal(state, e)
	}
	if e := db.First(&first, first.ID).Error; e != nil || first.Meta != "{invalid" {
		t.Fatal("unverified original was removed", e)
	}
	if resultTotal(t, db) != 10 {
		t.Fatal("holding proof changed settled usage")
	}
}

func TestExpiredGraphProjectionDoesNotReappearOnLateReportOrRebuild(t *testing.T) {
	db, accounting, reports, base := retentionReports(t)
	now := base.Add(370 * 24 * time.Hour)
	db.Config.NowFunc = func() time.Time { return now }
	if _, e := (LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	late := reports[0]
	late.EventID = "old-but-new-identity"
	late.BytesUp = 6
	late.Sequence = 8
	late.Revision = 8
	if _, _, e := accounting.Complete(t.Context(), late); e != nil {
		t.Fatal(e)
	}
	if _, e := (UsageProjectionWorker{DB: db}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if e := db.Transaction(datastore.RebuildTrafficHourly); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var n int64
		if e := db.Table(table).Count(&n).Error; e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
	if resultTotal(t, db) != 15 {
		t.Fatal("expired chart window blocked or repeated a charge")
	}
}
