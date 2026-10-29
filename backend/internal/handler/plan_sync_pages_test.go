package handler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const planSyncPagePopulation = 1200

type planSyncPageFixture struct {
	fullReplacementFixture
	plan        model.Plan
	rows        []model.Subscription
	group       model.NodeGroup
	newEndpoint model.ProtocolEndpoint
}

// One real wallet-funded purchase retains financial provenance. The remaining
// independent subscriptions are fixture metadata, not fabricated paid orders.
func newPlanSyncPageFixture(t *testing.T, base orderFixture) planSyncPageFixture {
	t.Helper()
	f := newFullReplacementFixture(t, base, false, 60, 0)
	var first model.Subscription
	if err := f.db.First(&first, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	copies := make([]model.Subscription, planSyncPagePopulation-1)
	for i := range copies {
		copies[i] = first
		// Sparse identities also exercise the primary-key cursor contract.
		copies[i].ID = first.ID + uint((i+1)*3)
	}
	if err := f.db.CreateInBatches(copies, 100).Error; err != nil {
		t.Fatal(err)
	}
	var rows []model.Subscription
	if err := f.db.Where("plan_id = ?", first.PlanID).Order("id ASC").Find(&rows).Error; err != nil || len(rows) != planSyncPagePopulation {
		t.Fatal("wrong subscription population", len(rows), err)
	}
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	for _, i := range []int{127, 128, 999, 1000, 1199} {
		if _, err := entitlementstore.EnsureCredentials(f.db, rows[i], issuer); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := planGroupEndpoint(t, f.db, "paged-new-scope")
	group := model.NodeGroup{Name: "Paged target", Code: "paged-target", IsEnabled: true}
	if err := f.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	var plan model.Plan
	if err := f.db.First(&plan, first.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	return planSyncPageFixture{f, plan, rows, group, endpoint}
}

type planSyncFinancialEvidence struct {
	Orders   []model.Order
	Events   []model.PaymentEvent
	Receipts []model.PaymentReceipt
	Funding  []model.OrderFunding
	Wallets  []model.WalletAccount
	Entries  []model.WalletEntry
}

func planSyncFinancialSnapshot(t *testing.T, db *gorm.DB) planSyncFinancialEvidence {
	t.Helper()
	var out planSyncFinancialEvidence
	for _, dest := range []any{&out.Orders, &out.Events, &out.Receipts, &out.Entries} {
		if err := db.Order("id ASC").Find(dest).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Order("order_id ASC").Find(&out.Funding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("user_id ASC, currency ASC").Find(&out.Wallets).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

// Enforce the memory/query bound while using the real transaction and routing.
// This fails if paging is replaced by a single unbounded or 1001-row Find.
func auditPlanSubscriptionPageReads(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	var pages atomic.Int64
	const hook = "test:plan-subscription-page-read-bound"
	if err := db.Callback().Query().After("gorm:query").Register(hook, func(q *gorm.DB) {
		if q.Error != nil || q.Statement.Table != "subscriptions" || q.RowsAffected <= 1 {
			return
		}
		limit, ok := q.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		if !ok || limit.Limit == nil || *limit.Limit > 128 || q.RowsAffected > 128 {
			q.AddError(errors.New("subscription sync exceeded its bounded page"))
			return
		}
		pages.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(hook) })
	return func() {
		db.Callback().Query().Remove(hook)
		if pages.Load() < 10 {
			t.Fatal("1200 subscriptions did not pass through bounded pages", pages.Load())
		}
	}
}

func assertPlanSyncPagedSuccess(t *testing.T, base orderFixture, group bool) {
	t.Helper()
	f := newPlanSyncPageFixture(t, base)
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	checkPages := auditPlanSubscriptionPageReads(t, f.db)
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: !group, ExpectedRevision: &f.plan.Revision}
	if group {
		request.NodeGroupID = &f.group.ID
	}
	updated, err := planQuotaUpdateService(f.db).Update(t.Context(), 1, f.plan.ID, request)
	if err != nil {
		t.Fatal("1200-subscription plan update failed", err)
	}
	checkPages()
	if updated.Revision != f.plan.Revision+1 {
		t.Fatal("plan revision not committed exactly once")
	}
	assertPlanSyncPagedRows(t, f, group, !group, 200, 0)
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Fatal("plan update rewrote immutable order/payment/wallet facts")
	}
	var intentsBefore, intentsAfter []model.NodeConfigPublish
	if err := f.db.Order("node_id").Find(&intentsBefore).Error; err != nil {
		t.Fatal(err)
	}
	var stale *commerce.PlanRevisionError
	if _, err := planQuotaUpdateService(f.db).Update(t.Context(), 1, f.plan.ID, request); !errors.As(err, &stale) {
		t.Fatal("stale revision replay was accepted", err)
	}
	if err := f.db.Order("node_id").Find(&intentsAfter).Error; err != nil || !reflect.DeepEqual(intentsBefore, intentsAfter) {
		t.Fatal("stale replay published again", err)
	}

}

func assertPlanSyncPagedRows(t *testing.T, f planSyncPageFixture, group, quota bool, total int64, meteredLast int64) {
	t.Helper()
	var after []model.Subscription
	if err := f.db.Where("plan_id = ?", f.plan.ID).Order("id ASC").Find(&after).Error; err != nil || len(after) != len(f.rows) {
		t.Fatal("subscriptions disappeared", err)
	}
	for i, row := range after {
		want := f.rows[i]
		if group {
			want.NodeGroupID = f.group.ID
		}
		if quota {
			want.FlowTotal, want.ResetQuotaBytes = want.CycleStartUsed+total, total
		}
		if i == len(after)-1 {
			want.FlowUsed += meteredLast
		}
		want.UpdatedAt = row.UpdatedAt
		if !reflect.DeepEqual(row, want) {
			t.Fatalf("subscription %d crossed quota/date/identity boundaries: got=%+v want=%+v", row.ID, row, want)
		}
	}
	var facts int64
	if quota {
		if err := f.db.Model(&model.QuotaEvent{}).Where("event_type = ? AND reference_type = ? AND reference_id = ?", "plan_quota_sync", "plan_revision", fmt.Sprintf("%d:%d", f.plan.ID, f.plan.Revision+1)).Count(&facts).Error; err != nil || facts != planSyncPagePopulation {
			t.Fatal("quota sync facts incomplete or duplicated", facts, err)
		}
	}
	if group {
		if err := f.db.Model(&model.AuditLog{}).Where("action = ?", "subscription.plan_group.sync").Count(&facts).Error; err != nil || facts != planSyncPagePopulation {
			t.Fatal("scope audit incomplete", facts, err)
		}
		var old []model.ProtocolCredential
		if err := f.db.Where("protocol_endpoint_id = ?", f.oldCredential.ProtocolEndpointID).Find(&old).Error; err != nil || len(old) != 6 {
			t.Fatal("old credential boundary fixture lost", len(old), err)
		}
		for _, credential := range old {
			if credential.Status != "revoked" || credential.RevokedAt == nil {
				t.Fatal("cross-page old authorization survived", credential.SubscriptionID)
			}
		}
		for _, node := range []uint{f.oldCredential.NodeID, f.newEndpoint.NodeID} {
			var intent model.NodeConfigPublish
			if err := f.db.First(&intent, node).Error; err != nil || intent.Generation == 0 || intent.Phase != "" || intent.AppliedConfigSHA256 != "" {
				t.Fatal("missing durable pending publication, or false applied confirmation", node, err)
			}
		}
	} else {
		var intent model.NodeConfigPublish
		if err := f.db.First(&intent, f.oldCredential.NodeID).Error; err != nil || intent.Generation < planSyncPagePopulation {
			t.Fatal("quota changes lost real publication intent", err)
		}
	}
}

func TestPlanSyncPagesCombinedChangeSupports1200(t *testing.T) {
	assertPlanSyncPagesCombined(t, newOrderFixture(t))
}
func TestMySQLPlanSyncPagesCombinedChangeSupports1200(t *testing.T) {
	assertPlanSyncPagesCombined(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanSyncPagesCombined(t *testing.T, base orderFixture) {
	f := newPlanSyncPageFixture(t, base)
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	quota := int64(200)
	_, err := planQuotaUpdateService(f.db).Update(t.Context(), 1, f.plan.ID, commerce.PlanUpdateRequest{NodeGroupID: &f.group.ID, TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &f.plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	assertPlanSyncPagedRows(t, f, true, true, quota, 0)
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Fatal("combined sync changed financial evidence")
	}
}

func TestPlanSyncPagesLateFailureRollsBackEveryPage(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(fmt.Sprint(group), func(t *testing.T) { assertPlanSyncPagesRollback(t, newOrderFixture(t), group, false) })
	}
}
func TestMySQLPlanSyncPagesLateFailureRollsBackEveryPage(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(fmt.Sprint(group), func(t *testing.T) { assertPlanSyncPagesRollback(t, newMySQLBusinessOrderFixture(t), group, false) })
	}
}
func TestPlanSyncPagesUnknownLateQuotaRollsBackCombinedChange(t *testing.T) {
	assertPlanSyncPagesRollback(t, newOrderFixture(t), true, true)
}
func TestMySQLPlanSyncPagesUnknownLateQuotaRollsBackCombinedChange(t *testing.T) {
	assertPlanSyncPagesRollback(t, newMySQLBusinessOrderFixture(t), true, true)
}
func assertPlanSyncPagesRollback(t *testing.T, base orderFixture, group, unknown bool) {
	f := newPlanSyncPageFixture(t, base)
	late := f.rows[1000]
	if unknown {
		// No immutable source supports this extra 17 bytes. Keep rejecting it;
		// an unknown adjustment cannot be attributed to an invented paid lot.
		if err := f.db.Model(&model.Subscription{}).Where("id = ?", late.ID).Update("flow_total", late.FlowTotal+17).Error; err != nil {
			t.Fatal(err)
		}
		// Update also writes UpdatedAt. Capture the complete persisted baseline
		// after the deliberate corruption, before entering PlanUpdate's TX;
		// retaining the old in-memory timestamp would falsely report a leak.
		if err := f.db.Where("plan_id = ?", f.plan.ID).Order("id ASC").Find(&f.rows).Error; err != nil || len(f.rows) != planSyncPagePopulation {
			t.Fatal("capture unknown-history rollback baseline", len(f.rows), err)
		}
		if f.rows[1000].ID != late.ID || f.rows[1000].FlowTotal != late.FlowTotal+17 {
			t.Fatal("unknown-history corruption was not persisted on the intended row")
		}
	}
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	var beforeIntents []model.NodeConfigPublish
	var beforeCredentials []model.ProtocolCredential
	if err := f.db.Order("node_id").Find(&beforeIntents).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("id").Find(&beforeCredentials).Error; err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	if !unknown {
		const hook = "test:plan-page-late-real-sql-failure"
		if err := f.db.Callback().Create().Before("gorm:create").Register(hook, func(q *gorm.DB) {
			match := false
			if event, ok := q.Statement.Dest.(*model.QuotaEvent); ok {
				match = !group && event.EventType == "plan_quota_sync" && event.SubscriptionID == late.ID
			}
			if audit, ok := q.Statement.Dest.(*model.AuditLog); ok {
				match = group && audit.Action == "subscription.plan_group.sync" && audit.Target == fmt.Sprintf("subscription:%d", late.ID)
			}
			if match {
				injected.Store(true)
				q.AddError(q.Exec("INSERT INTO zboard_test_missing_plan_sync_table(id) VALUES(1)").Error)
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer f.db.Callback().Create().Remove(hook)
	}
	quota := int64(200)
	request := commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: !group || unknown, ExpectedRevision: &f.plan.Revision}
	if group {
		request.NodeGroupID = &f.group.ID
	}
	_, err := planQuotaUpdateService(f.db).Update(t.Context(), 1, f.plan.ID, request)
	if unknown {
		var invalid *commerce.ValidationError
		if !errors.As(err, &invalid) || invalid.Fields["sync_traffic_quota"] == "" {
			t.Fatal("unknown history was fabricated", err)
		}
	} else if err == nil || !injected.Load() || !strings.Contains(err.Error(), "zboard_test_missing_plan_sync_table") {
		t.Fatal("late real SQL failure did not abort request", err)
	}
	var plan model.Plan
	var rows []model.Subscription
	var intents []model.NodeConfigPublish
	var credentials []model.ProtocolCredential
	if err := f.db.First(&plan, f.plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("plan_id = ?", f.plan.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("node_id").Find(&intents).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Order("id").Find(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	// Keep every field, including UpdatedAt, in the rollback assertion. Report
	// the failing domain and row instead of conflating a fixture mismatch with
	// a committed partial transaction.
	if !reflect.DeepEqual(plan, f.plan) {
		t.Errorf("late page failure changed plan: got=%+v want=%+v", plan, f.plan)
	}
	if !reflect.DeepEqual(rows, f.rows) {
		t.Errorf("late page failure changed subscription count/state: got=%d want=%d", len(rows), len(f.rows))
		for i := 0; i < len(rows) && i < len(f.rows); i++ {
			if !reflect.DeepEqual(rows[i], f.rows[i]) {
				t.Errorf("first changed subscription: got=%+v want=%+v", rows[i], f.rows[i])
				break
			}
		}
	}
	if !reflect.DeepEqual(intents, beforeIntents) {
		t.Errorf("late page failure changed publication intents: got=%+v want=%+v", intents, beforeIntents)
	}
	if !reflect.DeepEqual(credentials, beforeCredentials) {
		t.Errorf("late page failure changed credentials: got=%+v want=%+v", credentials, beforeCredentials)
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Error("late page failure changed immutable order/payment/wallet facts")
	}
	var facts int64
	if err := f.db.Model(&model.QuotaEvent{}).Where("event_type = ?", "plan_quota_sync").Count(&facts).Error; err != nil || facts != 0 {
		t.Fatal("rolled-back quota events escaped", facts, err)
	}
	if err := f.db.Model(&model.AuditLog{}).Where("action IN ?", []string{"subscription.plan_group.sync", "plan.update"}).Count(&facts).Error; err != nil || facts != 0 {
		t.Fatal("rolled-back audits escaped", facts, err)
	}
}

func TestPlanSyncPagesLocksLastPageBeforeFirstWrite(t *testing.T) {
	assertPlanSyncPagesMeteringRace(t, newOrderFixture(t))
}
func TestMySQLPlanSyncPagesLocksLastPageBeforeFirstWrite(t *testing.T) {
	assertPlanSyncPagesMeteringRace(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanSyncPagesMeteringRace(t *testing.T, base orderFixture) {
	f := newPlanSyncPageFixture(t, base)
	last := f.rows[len(f.rows)-1]
	var credential model.ProtocolCredential
	if err := f.db.Where("subscription_id = ? AND current_scope = ?", last.ID, 1).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	type marker struct{}
	entered, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	defer unblock.Do(func() { close(release) })
	const hook = "test:plan-page-last-authority-lock"
	if err := f.db.Callback().Update().After("gorm:update").Register(hook, func(q *gorm.DB) {
		if q.Error == nil && q.Statement.Context.Value(marker{}) == true && q.Statement.Table == "subscriptions" {
			once.Do(func() {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					q.AddError(ctx.Err())
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Update().Remove(hook)
	done, metered := make(chan error, 1), make(chan error, 1)
	quota := int64(200)
	go func() {
		_, err := planQuotaUpdateService(f.db).Update(context.WithValue(ctx, marker{}, true), 1, f.plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &f.plan.Revision})
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("sync never reached first write: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	waits := pool.Stats().WaitCount
	accounting := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: f.db, Cipher: base.h.credentialCipher, EnqueuePublications: networkstore.EnqueueSubscriptionPublications}}
	event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "plan-page-race", EventID: "plan-page-last-25", EventType: "flow.completed", Sequence: 1, FlowID: "plan-page-flow", PrincipalKey: credential.PrincipalKey, BytesDown: 25, OccurredAt: *f.at}
	go func() { _, _, err := accounting.Complete(ctx, event); metered <- err }()
	blocked := false
	for !blocked && ctx.Err() == nil {
		if f.db.Dialector.Name() == "sqlite" {
			blocked = pool.Stats().WaitCount > waits
		} else {
			var count int64
			if err := f.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA = DATABASE() AND l.OBJECT_NAME = 'subscriptions' AND l.INDEX_NAME = 'PRIMARY' AND l.LOCK_DATA = ?", fmt.Sprint(last.ID)).Scan(&count).Error; err != nil {
				t.Fatal(err)
			}
			blocked = count > 0
		}
		if !blocked {
			select {
			case err := <-metered:
				t.Fatalf("last-page writer was not held before first write: %v", err)
			case <-time.After(time.Millisecond):
			case <-ctx.Done():
			}
		}
	}
	unblock.Do(func() { close(release) })
	if !blocked {
		t.Fatal("last-page authority was not locked before first page side effects")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-metered; err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounting.Complete(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	assertPlanSyncPagedRows(t, f, false, true, quota, 25)
	var records int64
	if err := f.db.Model(&model.TrafficRecord{}).Where("report_id = ?", event.EventID).Count(&records).Error; err != nil || records != 1 {
		t.Fatal("cross-page metering replay was lost or duplicated", records, err)
	}
}

func TestPlanSyncPagesLoweringQuotaPreserves1200UsedCounters(t *testing.T) {
	assertPlanSyncPagesLowerQuota(t, newOrderFixture(t))
}
func TestMySQLPlanSyncPagesLoweringQuotaPreserves1200UsedCounters(t *testing.T) {
	assertPlanSyncPagesLowerQuota(t, newMySQLBusinessOrderFixture(t))
}
func assertPlanSyncPagesLowerQuota(t *testing.T, base orderFixture) {
	f := newPlanSyncPageFixture(t, base)
	beforeMoney := planSyncFinancialSnapshot(t, f.db)
	quota := int64(40)
	if _, err := planQuotaUpdateService(f.db).Update(t.Context(), 1, f.plan.ID, commerce.PlanUpdateRequest{TrafficBytes: &quota, SyncTrafficQuota: true, ExpectedRevision: &f.plan.Revision}); err != nil {
		t.Fatal(err)
	}
	assertPlanSyncPagedRows(t, f, false, true, quota, 0)
	var credentials []model.ProtocolCredential
	if err := f.db.Find(&credentials).Error; err != nil || len(credentials) != 6 {
		t.Fatal(err)
	}
	for _, credential := range credentials {
		if credential.Status != "expired" {
			t.Fatal("lowered quota did not persist stop intent", credential.SubscriptionID)
		}
	}
	if !reflect.DeepEqual(beforeMoney, planSyncFinancialSnapshot(t, f.db)) {
		t.Fatal("lowering quota changed financial facts")
	}
}
