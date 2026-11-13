package datastore_test

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestMySQLMeteringRetentionColdCollationAndLateContribution(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	source := diagnosticSchemaSource(t, db, false)
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-48 * time.Hour)
	group := model.NodeGroup{Name: "retention", Code: "retention"}
	if e := db.Create(&group).Error; e != nil {
		t.Fatal(e)
	}
	plan := model.Plan{Name: "retention", Slug: "retention", NodeGroupID: group.ID, IsActive: true}
	if e := db.Create(&plan).Error; e != nil {
		t.Fatal(e)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "retention", Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", IsActive: true}
	if e := db.Create(&sku).Error; e != nil {
		t.Fatal(e)
	}
	sub := model.Subscription{UserID: source.UserID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, StartAt: old, EndAt: now.AddDate(1, 0, 0), Status: "active", FlowTotal: 1000, FlowUsed: source.UsedBytes, Config: "{}"}
	if e := db.Create(&sub).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: source.ProtocolEndpointID}).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&model.Node{}).Where("id=?", source.NodeID).Update("traffic_secret", "fixture-secret").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&source).Updates(map[string]any{"subscription_id": sub.ID, "report_id": "RÉPLAY-OLD", "created_at": old, "updated_at": old, "record_at": old, "meta": ""}).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.First(&source, source.ID).Error; e != nil {
		t.Fatal(e)
	}
	// New row admitted after fixed migration prefix; its settled deltas and flag
	// commit together, and retain the exact MySQL case/accent identity rules.
	if e := db.Transaction(func(tx *gorm.DB) error {
		return meteringstore.RecordUsageProjections(tx, []model.TrafficRecord{source})
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.UsageProjectionWorker{DB: db}).Drain(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	if n, e := (meteringstore.LedgerRetention{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 100); e != nil || n != 1 {
		var rows []model.TrafficRecord
		db.Find(&rows)
		var p []model.MeteringUsageProjectionPending
		db.Find(&p)
		t.Fatalf("retained=%d err=%v rows=%+v pending=%+v", n, e, rows, p)
	}
	if e := db.Model(&model.MeteringReportReceipt{}).Where("node_id=?", source.NodeID).UpdateColumn("created_at", old).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := (meteringstore.ColdIdentities{DB: db, Now: func() time.Time { return now }}).Drain(t.Context(), 32); e != nil {
		t.Fatal(e)
	}
	var hot int64
	db.Model(&model.MeteringReportReceipt{}).Where("node_id=?", source.NodeID).Count(&hot)
	if hot != 0 {
		t.Fatal("hot receipt remained", hot)
	}
	if e := datastore.RunMigrations(db); e != nil {
		t.Fatal("restart schema", e)
	}
	adapter := meteringstore.NodeReports{DB: db, Expire: func(*gorm.DB, uint, time.Time) error { return nil }, QuotaEvent: func(*gorm.DB, model.Subscription, string, int64, int64, int64, string, string) error { return nil }}
	input := metering.AuthenticatedNodeReport{NodeID: source.NodeID, UserID: source.UserID, ProtocolEndpointID: source.ProtocolEndpointID, ReportID: "replay-old", Nonce: "fresh-duplicate", ExpectedCredential: "fixture-secret", Timestamp: now, RawBytes: 3, UploadBytes: 3}
	replay, e := adapter.Record(t.Context(), input)
	if e != nil || !replay.Duplicate {
		t.Fatal("cold MySQL case/accent replay", replay, e)
	}
	input.ReportID = "nonce-reuse"
	input.Nonce = strings.ToUpper(source.Nonce)
	if _, e = adapter.Record(t.Context(), input); !errors.Is(e, metering.ErrNodeReportNonceReplayed) {
		t.Fatal("cold nonce alias replay was admitted", e)
	}
	input.ReportID = "late-unseen"
	input.Nonce = "new-nonce"
	if _, e = adapter.Record(t.Context(), input); e != nil {
		t.Fatal("unseen report after transfer", e)
	}
	if replay, e = adapter.Record(t.Context(), input); e != nil || !replay.Duplicate {
		t.Fatal("new replay", e)
	}
	var result model.MeteringSettlementResult
	if e := db.Where("subscription_id=?", source.SubscriptionID).First(&result).Error; e != nil || result.UsedBytes != source.UsedBytes+5 || result.RawBytes != source.RawBytes+3 {
		t.Fatal(result, e)
	}
	var root model.MeteringColdRoot
	if e := db.Where("node_id=? AND kind=1", source.NodeID).First(&root).Error; e != nil || root.EntryCount != 1 {
		t.Fatal(root, e)
	}
}
