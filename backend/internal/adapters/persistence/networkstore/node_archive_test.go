package networkstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observabilitystore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/publicationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestNodeArchivePreservesFinancialRowsWithoutAccessingHistory(t *testing.T) {
	archivePreservesHistory(t, 519)
}
func TestMySQLNodeArchivePreservesFinancialRowsWithoutAccessingHistory(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("requires disposable MySQL")
	}
	total := 519
	if scale := os.Getenv("ZBOARD_RETIREMENT_SCALE_ROWS"); scale != "" {
		var err error
		total, err = strconv.Atoi(scale)
		if err != nil || total < 519 || total > 200000 {
			t.Fatal("invalid scale")
		}
	}
	archivePreservesHistory(t, total)
}
func archivePreservesHistory(t *testing.T, total int) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	if err := db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Config: "{}", StartAt: now, EndAt: now.Add(time.Hour)},
		&model.Node{ID: 2, Name: "surviving", Config: "{}"},
		&model.ProtocolEndpoint{ID: 2, NodeID: 2, Name: "surviving", RuntimeKey: "surviving", Protocol: "vless", OptionalConfig: "{}", Tags: "[]"},
		&model.ProtocolEndpoint{ID: 3, NodeID: 1, Name: "retiring-extra", RuntimeKey: "retiring-extra", Protocol: "vless", OptionalConfig: "{}", Tags: "[]"},
		&model.ProtocolCredential{ID: 1, NodeID: 1, ProtocolEndpointID: 1, SubscriptionID: 1, UserID: 1, CredentialID: "retiring", PrincipalKey: "retiring", ExpiresAt: now.Add(time.Hour)},
		&model.ProtocolCredential{ID: 2, NodeID: 2, ProtocolEndpointID: 2, SubscriptionID: 1, UserID: 1, CredentialID: "surviving", PrincipalKey: "surviving", ExpiresAt: now.Add(time.Hour)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	flows := make([]model.FlowUsage, total+1)
	ledger := make([]model.TrafficRecord, total+1)
	for i := range flows {
		node := uint(1)
		if i == total {
			node = 2
		}
		key := fmt.Sprintf("flow-%d", i)
		flows[i] = model.FlowUsage{NodeID: node, ProtocolEndpointID: node, ProtocolCredentialID: node, SubscriptionID: 1, FlowID: key, PrincipalKey: key, LastEventID: key, LastSeenAt: now}
		ledger[i] = model.TrafficRecord{NodeID: node, ProtocolEndpointID: node, SubscriptionID: 1, UserID: 1, ReportID: key, Nonce: key, FlowID: key, UsedBytes: 7, RawBytes: 5, UploadBytes: 3, DownloadBytes: 2, At: now, CreatedAt: now, UpdatedAt: now}
		if i < total && i%2 == 1 {
			ledger[i].ProtocolEndpointID = 3
		}
	}
	for _, rows := range []any{&flows, &ledger} {
		if err := db.CreateInBatches(rows, 100).Error; err != nil {
			t.Fatal(err)
		}
	}
	receipt := model.MeteringReportReceipt{NodeID: 1, ReportID: ledger[0].ReportID, ProtocolEndpointID: 1, TrafficRecordID: ledger[0].ID, CreatedAt: now}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}

	var beforeLedger []model.TrafficRecord
	var beforeFlows []model.FlowUsage
	if err := db.Order("id").Find(&beforeLedger).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&beforeFlows).Error; err != nil {
		t.Fatal(err)
	}
	rejectHistory := func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case "traffic_records", "flow_usages", "metering_report_receipts":
			tx.AddError(errors.New("retirement touched lifetime history"))
		}
	}
	db.Callback().Query().Before("gorm:query").Register("archive:no_history", rejectHistory)
	db.Callback().Update().Before("gorm:update").Register("archive:no_history", rejectHistory)
	db.Callback().Delete().Before("gorm:delete").Register("archive:no_history", rejectHistory)
	if err := db.Create(&model.NodeConfigPublish{NodeID: 1, Generation: 1, LeaseUntil: now, NextAttemptAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeProxyPool{NodeID: 1, Name: "archive pool", Config: "{}", SubscriptionURL: "encrypted:subscription-secret", AutoSync: true, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	store := NodeRetirement{DB: db}
	if _, err := store.Start(t.Context(), 1, 1, "detach"); err != nil {
		t.Fatal(err)
	}
	remote := &retirementRemoteFixture{store: store, calls: map[string]int{}}
	start := time.Now()
	if err := executeRetirement(db, network.NodeRetirement{Repository: store, Remote: remote}); err != nil {
		t.Fatal(err)
	}
	t.Logf("archived %d historical flows and ledger rows in %s without querying either table", total, time.Since(start))
	db.Callback().Query().Remove("archive:no_history")
	db.Callback().Update().Remove("archive:no_history")
	db.Callback().Delete().Remove("archive:no_history")
	if len(remote.calls) != 0 {
		t.Fatal("detach touched remote resources")
	}
	var afterLedger []model.TrafficRecord
	var afterFlows []model.FlowUsage
	if err := db.Order("id").Find(&afterLedger).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterFlows).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeLedger, afterLedger) || !reflect.DeepEqual(beforeFlows, afterFlows) {
		t.Fatal("archive changed historical rows, financial facts, or original FKs")
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.LifecycleStatus != "archived" || node.ArchivedAt == nil || node.IsEnabled || node.NodeCredentialRevokedAt == nil || node.TrafficSecretRevokedAt == nil || node.SSHPwd != "" {
		t.Fatal("missing archive authority", node, err)
	}
	for _, id := range []uint{1, 2} {
		err := db.Transaction(func(tx *gorm.DB) error { return credentialstore.LockCurrentEndpointParents(tx, id, id) })
		if id == 1 && !errors.Is(err, network.ErrNodeAdministrationDeleting) {
			t.Fatal("archive admits accounting", err)
		}
		if id == 2 && err != nil {
			t.Fatal("other node rejected", err)
		}
	}
	inventory := Inventory{DB: db}
	page, err := inventory.ListNodes(t.Context(), network.NodeInventoryQuery{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Node.ID != 2 {
		t.Fatal("default inventory includes archive", page, err)
	}
	page, err = inventory.ListNodes(t.Context(), network.NodeInventoryQuery{LifecycleStatus: "archived"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Node.ID != 1 {
		t.Fatal("archive history unavailable", page, err)
	}
	active, enabled := "active", true
	if _, err := (NodeAdministration{DB: db}).UpdateNodeAdministration(t.Context(), 1, network.NodeUpdateRequest{ID: 1, LifecycleStatus: &active, IsEnabled: &enabled}); !errors.Is(err, network.ErrNodeAdministrationDeleting) {
		t.Fatal("archive was reactivated", err)
	}
	var retained model.MeteringReportReceipt
	if err := db.First(&retained, "node_id = ? AND report_id = ?", receipt.NodeID, receipt.ReportID).Error; err != nil || retained.TrafficRecordID != receipt.TrafficRecordID {
		t.Fatal("immutable receipt lost", err)
	}
	var pool model.NodeProxyPool
	if err := db.Where("node_id = 1").First(&pool).Error; err != nil || pool.AutoSync || pool.SubscriptionURL != "" || pool.Revision != 2 {
		t.Fatal("archive retained a live proxy subscription", pool, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return publicationstore.Enqueue(tx, 1, 1, 1) }); err != nil {
		t.Fatal("historical reconciliation blocked", err)
	}
	var publication model.NodeConfigPublish
	if err := db.First(&publication, "node_id = 1").Error; err != nil || !publication.Suspended || publication.Generation != 1 {
		t.Fatal("archive recreated current publication", publication, err)
	}
	retention := observabilitystore.HistoryRetention{DB: db}
	if n, err := retention.Prune(t.Context(), observability.ArchivedFlowCursors, node.ArchivedAt.Add(-time.Hour), 256); err != nil || n != 0 {
		t.Fatal("archive grace was bypassed", n, err)
	}
	eligible := node.ArchivedAt.Add(time.Hour)
	failure := errors.New("GC write failed")
	db.Callback().Delete().Before("gorm:delete").Register("archive:gc_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "flow_usages" {
			tx.AddError(failure)
		}
	})
	if _, err := retention.Prune(t.Context(), observability.ArchivedFlowCursors, eligible, 256); !errors.Is(err, failure) {
		t.Fatal("GC failure hidden", err)
	}
	db.Callback().Delete().Remove("archive:gc_failure")
	for range 2 {
		if n, err := retention.Prune(t.Context(), observability.ArchivedFlowCursors, eligible, 256); err != nil || n != 256 {
			t.Fatal("GC batch did not resume within bound", n, err)
		}
	}
	var remaining int64
	if err := db.Model(&model.FlowUsage{}).Where("node_id = 1").Count(&remaining).Error; err != nil || remaining != int64(total-512) {
		t.Fatal("GC exceeded bound or lost work", remaining, err)
	}
	if err := db.Model(&model.FlowUsage{}).Where("node_id = 2").Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatal("GC removed a live cursor", remaining, err)
	}
	afterLedger = nil
	if err := db.Order("id").Find(&afterLedger).Error; err != nil || !reflect.DeepEqual(beforeLedger, afterLedger) {
		t.Fatal("GC changed financial history", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return credentialstore.LockCurrentEndpointParents(tx, 1, 1) }); !errors.Is(err, network.ErrNodeAdministrationDeleting) {
		t.Fatal("cursor reclamation reopened accounting", err)
	}

}

func TestArchiveFinalizationRetainsActualCancellationAndLeaseFences(t *testing.T) {
	db, store, _, _ := retirementFixture(t)
	first, err := store.Start(t.Context(), 1, 1, "detach")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := jobstore.New(db).Claim(t.Context(), "archive-test", []string{network.NodeRetirementHandler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(t.Context(), 1, "stale-run"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("stale archive admitted", err)
	}
	if _, err := jobstore.New(db).RecordCancellation(t.Context(), jobs.Reviewer{AccountID: 1}, jobs.Cancellation{RunID: first.RunID, Reason: "cancel archive"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(t.Context(), 1, first.RunID); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("canceled archive admitted", err)
	}
	if err := jobstore.New(db).Finish(t.Context(), claim, jobs.Canceled); err != nil {
		t.Fatal(err)
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.ArchivedAt != nil {
		t.Fatal("canceled archive mutated identity", err)
	}
}
