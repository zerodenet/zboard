package networkstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRetirementBatchesResumeAndPreserveLedger(t *testing.T) {
	retirementBatchesResume(t, 2*retirementBatchSize+7)
}

func TestMySQLRetirementBatchesResumeAndPreserveLedger(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("requires disposable MySQL")
	}
	total := 2*retirementBatchSize + 7
	if scale := os.Getenv("ZBOARD_RETIREMENT_SCALE_ROWS"); scale != "" {
		var err error
		total, err = strconv.Atoi(scale)
		if err != nil || total < 2*retirementBatchSize+7 || total > 200000 {
			t.Fatal("invalid disposable retirement scale")
		}
	}
	retirementBatchesResume(t, total)
}

func retirementBatchesResume(t *testing.T, total int) {
	t.Helper()
	db, other := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	if err := db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Config: "{}", StartAt: now, EndAt: now.Add(time.Hour)},
		&model.Node{ID: 2, Name: "surviving", Config: "{}"},
		&model.ProtocolEndpoint{ID: 2, NodeID: 2, Name: "surviving", RuntimeKey: "surviving", Protocol: "vless", OptionalConfig: "{}", Tags: "[]"},
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
	store := NodeRetirement{DB: db}
	first, err := store.Start(t.Context(), 1, 1, "detach")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := jobstore.New(db).Claim(t.Context(), "batch-test", []string{network.NodeRetirementHandler}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.removalBatch(t.Context(), 1, "stale-run", "flow_cursors"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("stale cleanup admitted", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return credentialstore.LockCurrentEndpointParents(tx, 1, 1) }); !errors.Is(err, network.ErrNodeAdministrationDeleting) {
		t.Fatal("retiring node can still recreate accounting cursors", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return credentialstore.LockCurrentEndpointParents(tx, 2, 2) }); err != nil {
		t.Fatal("ordinary disabled node lost historical accounting", err)
	}
	if done, err := store.removalBatch(t.Context(), 1, first.RunID, "flow_cursors"); err != nil || done {
		t.Fatal(done, err)
	}
	var remaining int64
	db.Model(&model.FlowUsage{}).Where("node_id = 1").Count(&remaining)
	if remaining != int64(total-retirementBatchSize) {
		t.Fatal("batch did not obey its row bound", remaining)
	}
	// A distinct connection can acquire the actual shared ledger between batches.
	if err := jobstore.New(other).WithLedgerLock(t.Context(), func(*gorm.DB) error { return nil }); err != nil {
		t.Fatal("cleanup retained the admission lock", err)
	}
	cleaned := retirementBatchSize
	if db.Dialector.Name() == "mysql" {
		retirementBatchWhileAdmissionBusy(t, store, other, first.RunID)
		cleaned += retirementBatchSize
	}
	injected := errors.New("audit unavailable")
	if err := db.Callback().Create().Before("gorm:create").Register("reject_retirement_batch_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.removalBatch(t.Context(), 1, first.RunID, "flow_cursors")
	db.Callback().Create().Remove("reject_retirement_batch_audit")
	if !errors.Is(err, injected) {
		t.Fatal("unaudited batch committed", err)
	}
	db.Model(&model.FlowUsage{}).Where("node_id = 1").Count(&remaining)
	if remaining != int64(total-cleaned) {
		t.Fatal("failed batch escaped rollback", remaining)
	}
	if _, err := jobstore.New(db).RecordCancellation(t.Context(), jobs.Reviewer{AccountID: 1}, jobs.Cancellation{RunID: first.RunID, Reason: "verify cleanup cancellation fence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.removalBatch(t.Context(), 1, first.RunID, "flow_cursors"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("canceled cleanup admitted another batch", err)
	}
	if err := jobstore.New(db).Finish(t.Context(), claim, jobs.Canceled); err != nil {
		t.Fatal(err)
	}
	store = NodeRetirement{DB: other}
	if _, err := store.Start(t.Context(), 1, 1, "detach"); err != nil {
		t.Fatal(err)
	}
	remote := &retirementRemoteFixture{store: store, calls: map[string]int{}}
	if err := executeRetirement(other, network.NodeRetirement{Repository: store, Remote: remote}); err != nil {
		t.Fatal(err)
	}
	if len(remote.calls) != 0 {
		t.Fatal("detach performed remote work")
	}
	var after []model.TrafficRecord
	if err := db.Order("id").Find(&after).Error; err != nil || len(after) != len(ledger) {
		t.Fatal("ledger rows were deleted", len(after), err)
	}
	for i, row := range after {
		before := ledger[i]
		if row.ID != before.ID || row.ReportID != before.ReportID || row.UsedBytes != before.UsedBytes || row.RawBytes != before.RawBytes || row.UploadBytes != before.UploadBytes || row.DownloadBytes != before.DownloadBytes || !row.UpdatedAt.Equal(before.UpdatedAt) || !row.CreatedAt.Equal(before.CreatedAt) {
			t.Fatal("ledger financial fact or timestamp changed", row.ID)
		}
		if i < total && (row.NodeID != 0 || row.ProtocolEndpointID != 0) {
			t.Fatal("retired references remain", row.ID)
		}
	}
	if last := after[total]; last.NodeID != 2 || last.ProtocolEndpointID != 2 {
		t.Fatal("surviving node references changed")
	}
	var retained model.MeteringReportReceipt
	if err := db.First(&retained, "node_id = ? AND report_id = ?", receipt.NodeID, receipt.ReportID).Error; err != nil || retained.NodeID != receipt.NodeID || retained.ReportID != receipt.ReportID || retained.ProtocolEndpointID != receipt.ProtocolEndpointID || retained.TrafficRecordID != receipt.TrafficRecordID || !retained.CreatedAt.Equal(receipt.CreatedAt) {
		t.Fatal("immutable report receipt changed", err)
	}
	var count int64
	db.Model(&model.FlowUsage{}).Where("node_id = 2").Count(&count)
	if count != 1 {
		t.Fatal("other node cursor deleted")
	}
}

func retirementBatchWhileAdmissionBusy(t *testing.T, store NodeRetirement, other *gorm.DB, runID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unlock()
	held := make(chan error, 1)
	go func() {
		held <- jobstore.New(other).WithLedgerLock(ctx, func(*gorm.DB) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("admission lock was not acquired", ctx.Err())
	}
	finished := make(chan error, 1)
	go func() {
		_, err := store.removalBatch(ctx, 1, runID, "flow_cursors")
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("already-admitted cleanup waited behind unrelated admission")
	}
	unlock()
	if err := <-held; err != nil {
		t.Fatal(err)
	}
}
