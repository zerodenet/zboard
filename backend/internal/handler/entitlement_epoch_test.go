package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRecoveredEntitlementKeepsLateUsageAndRevocationInOldEpoch(t *testing.T) {
	assertRecoveredEntitlementEpoch(t, newOrderFixture(t))
}
func TestMySQLRecoveredEntitlementKeepsLateUsageAndRevocationInOldEpoch(t *testing.T) {
	assertRecoveredEntitlementEpoch(t, newMySQLOrderFixture(t))
}
func assertRecoveredEntitlementEpoch(t *testing.T, base orderFixture) {
	attachOrderPublishEndpoint(t, base)
	f := newRenewalValueFixture(t, base)
	h := base.h
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", f.subscription).Update("flow_used", int64(10)).Error; err != nil {
		t.Fatal(err)
	}
	var old model.ProtocolCredential
	if err := h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&old).Error; err != nil {
		t.Fatal(err)
	}
	plain, err := h.credentialCipher.Decrypt(old.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var token model.SubscriptionToken
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.First(&sub, f.subscription).Error; err != nil {
			return err
		}
		var err error
		token, _, err = application.EnsureSubscriptionAccessToken(tx, sub, h.credentialCipher)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(old, 0, 1)
	events[0].PrincipalKey = plain
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	var before model.Subscription
	if err := h.db.First(&before, f.subscription).Error; err != nil || before.FlowUsed != 40 {
		t.Fatalf("pre-recovery usage %d %v", before.FlowUsed, err)
	}
	*f.at = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	pending := f.create(t, f.monthly, f.subscription, "recover-epoch")
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	// A failed new credential must roll back the closed quota, source wipe,
	// wallet capture, entitlement revision and durable publication intent.
	var pendingBefore model.NodeConfigPublish
	if err := h.db.First(&pendingBefore, old.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	trigger := `CREATE TRIGGER reject_epoch_credential BEFORE INSERT ON protocol_credentials WHEN NEW.entitlement_epoch = 1 BEGIN SELECT RAISE(ABORT,'fresh credential unavailable'); END`
	if !datastore.IsSQLite(h.db) {
		trigger = `CREATE TRIGGER reject_epoch_credential BEFORE INSERT ON protocol_credentials FOR EACH ROW BEGIN IF NEW.entitlement_epoch = 1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fresh credential unavailable'; END IF; END`
	}
	if err := h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Pay(t.Context(), 1, pending.ID); err == nil {
		t.Fatal("failed credential committed fresh period")
	}
	var closedCount int64
	if err := h.db.Model(&model.ClosedEntitlement{}).Count(&closedCount).Error; err != nil || closedCount != 0 {
		t.Fatalf("failed payment left closed quota %d %v", closedCount, err)
	}
	var preserved model.ProtocolCredential
	if err := h.db.First(&preserved, old.ID).Error; err != nil || preserved.Secret != old.Secret || preserved.CurrentScope == nil {
		t.Fatal("failed transition destroyed old identity", err)
	}
	var pendingAfter model.NodeConfigPublish
	if err := h.db.First(&pendingAfter, old.NodeID).Error; err != nil || pendingAfter.Generation != pendingBefore.Generation || pendingAfter.Reconciling != pendingBefore.Reconciling {
		t.Fatal("failed transition changed durable publication intent", err)
	}
	var failedOrder model.Order
	if err := h.db.First(&failedOrder, pending.ID).Error; err != nil || failedOrder.Status != "pending" || failedOrder.FulfilledAt != nil || failedOrder.EntitlementValueSnapshot != "" {
		t.Fatal("failed transition committed paid/fulfillment evidence", err)
	}
	walletMatches(t, base, 18000, 1000, 4)
	if err := h.db.Exec("DROP TRIGGER reject_epoch_credential").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, pending.ID); err != nil {
			t.Fatal(err)
		}
	}
	var current model.Subscription
	if err := h.db.First(&current, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(current))
	if current.EntitlementEpoch != 1 || current.FlowUsed != 40 || total != 100 || used != 0 || !current.StartAt.Equal(*f.at) {
		t.Fatalf("fresh period epoch/quota %+v total=%d used=%d", current, total, used)
	}
	var afterToken model.SubscriptionToken
	if err := h.db.First(&afterToken, token.ID).Error; err != nil || afterToken.TokenHash != token.TokenHash || afterToken.TokenCiphertext != token.TokenCiphertext || afterToken.RevokedAt != nil {
		t.Fatal("recovery changed the subscription URL", err)
	}
	if err := h.db.First(&preserved, old.ID).Error; err != nil || preserved.CurrentScope != nil || preserved.Status != "revoked" || preserved.Secret != "" || preserved.RetiredPrincipalHash != entitlements.RetiredPrincipalHash(old.NodeID, plain) {
		t.Fatal("old identity not retired safely", err)
	}
	var fresh model.ProtocolCredential
	if err := h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	freshPlain, err := h.credentialCipher.Decrypt(fresh.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == old.ID || fresh.PrincipalKey == old.PrincipalKey || freshPlain == plain || fresh.EntitlementEpoch != 1 || !fresh.DeliveryRequired {
		t.Fatal("fresh quota reused old wire identity")
	}
	// Replay a preparation job's original snapshot after recovery. It may
	// extend only the current slot, never resurrect the retired first period.
	reconciled, err := application.EnsureSubscriptionCredentials(h.db, before, h.credentialCipher, h.zeroMieruAccess)
	if err != nil || len(reconciled) != 1 || reconciled[0].ID != fresh.ID || reconciled[0].EntitlementEpoch != 1 || !reconciled[0].ExpiresAt.Equal(current.EndAt) {
		t.Fatal("stale preparation reissued an old epoch", err)
	}
	// Old principal remains forbidden even though the same group is currently
	// authorized for this recovered subscription. The new principal is retained.
	calls := 0
	guard := networkstore.ConnectionRevocation{DB: h.db, Cipher: h.credentialCipher}
	if forbidden, err := guard.WithRevokedPrincipal(t.Context(), old.NodeID, plain, func() error { calls++; return nil }); err != nil || !forbidden || calls != 1 {
		t.Fatalf("old epoch kept its connection %v %d %v", forbidden, calls, err)
	}
	if forbidden, err := guard.WithRevokedPrincipal(t.Context(), fresh.NodeID, freshPlain, func() error { t.Fatal("closed fresh principal"); return nil }); err != nil || forbidden {
		t.Fatal("fresh authorization rejected", err)
	}
	// Buffered old flow advances its old cursor; repeated final completion
	// exhausts only the old100-byte budget, with no current quota mutation.
	late := accountingBenchmarkEvents(old, 1, 1)
	late[0].PrincipalKey = plain
	for range 2 {
		if err := h.projectZeroNodeEvents(t.Context(), late); err != nil {
			t.Fatal(err)
		}
	}
	final := metering.CompletedFlow{NodeID: old.NodeID, CoreInstanceID: "benchmark-core", EventID: "closed-epoch-completion", EventType: "flow.completed", Sequence: 3, FlowID: "flow-0", PrincipalKey: plain, BytesUp: 60, BytesDown: 120, OccurredAt: *f.at}
	for range 2 {
		if _, exhausted, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), final); err != nil || exhausted {
			t.Fatalf("old final exhausted new quota %v %v", exhausted, err)
		}
	}
	var closed model.ClosedEntitlement
	if err := h.db.Where("subscription_id = ? AND epoch = ?", f.subscription, 0).Take(&closed).Error; err != nil || closed.FlowUsed != 100 || closed.ClosingFlowUsed != 40 || closed.FlowTotal != 100 || closed.TransitionOrderID != pending.ID {
		t.Fatalf("old quota attribution %+v %v", closed, err)
	}
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.FlowUsed != 40 || current.FlowTotal != 140 || current.Status != "active" {
		t.Fatalf("late old usage changed fresh quota %+v %v", current, err)
	}
	var oldUsed int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id = ? AND entitlement_epoch = ?", f.subscription, 0).Select("COALESCE(SUM(used_bytes),0)").Scan(&oldUsed).Error; err != nil || oldUsed != 90 {
		t.Fatalf("old quota ledger expected90 plus pre-existing10, got %d %v", oldUsed, err)
	}
	// Current principal uses an independent native runtime generation and is the
	// only route that advances current quota after the transition.
	active := accountingBenchmarkEvents(fresh, 0, 1)
	active[0].CoreInstanceID = "recovered-core"
	active[0].ID = "recovered-sample"
	if err := h.projectZeroNodeEvents(t.Context(), active); err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.FlowUsed != 70 {
		t.Fatalf("fresh usage missing %d %v", current.FlowUsed, err)
	}
	// The old user/endpoint-only reporting contract lacks the immutable period
	// identity and must explain that limitation rather than bill the fresh quota.
	if err := h.db.Model(&model.Node{}).Where("id = ?", fresh.NodeID).Updates(map[string]any{"traffic_secret": "epoch-fixture-traffic-key", "traffic_secret_revoked_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	report := metering.AuthenticatedNodeReport{NodeID: fresh.NodeID, UserID: fresh.UserID, ProtocolEndpointID: fresh.ProtocolEndpointID, ExpectedCredential: "epoch-fixture-traffic-key", Timestamp: time.Now().UTC(), Nonce: "epoch-legacy-nonce", ReportID: "epoch-legacy-report", DownloadBytes: 50}
	if _, err := h.services.NodeReports().Record(t.Context(), report); !errors.Is(err, metering.ErrNodeReportNeedsPrincipal) {
		t.Fatalf("legacy report charged fresh period %v", err)
	}
	// A later early renewal extends the existing period without another key
	// rotation or quota reset. Missing closed evidence must never bill new quota.
	early := f.create(t, f.monthly, f.subscription, "early-same-epoch")
	f.pay(t, early.ID)
	var retained model.ProtocolCredential
	if err := h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&retained).Error; err != nil || retained.ID != fresh.ID || retained.Secret != fresh.Secret {
		t.Fatal("early renewal rotated credentials", err)
	}
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.EntitlementEpoch != 1 || current.FlowUsed != 70 {
		t.Fatal("early renewal changed quota/epoch", err)
	}
	// A second genuine recovery closes epoch1 without reusing either previous
	// identity or the first closed counter. Historical attribution remains exact.
	*f.at = current.EndAt.Add(24 * time.Hour)
	secondRecovery := f.create(t, f.monthly, f.subscription, "second-recovered-epoch")
	f.pay(t, secondRecovery.ID)
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.EntitlementEpoch != 2 || current.FlowUsed != 70 {
		t.Fatal("second recovery reused a period", err)
	}
	var secondFresh model.ProtocolCredential
	if err := h.db.Where("subscription_id = ? AND current_scope = ?", f.subscription, 1).First(&secondFresh).Error; err != nil || secondFresh.ID == fresh.ID || secondFresh.EntitlementEpoch != 2 {
		t.Fatal("second recovery reused a credential", err)
	}
	oldOne := accountingBenchmarkEvents(fresh, 1, 1)
	oldOne[0].CoreInstanceID = "recovered-core"
	oldOne[0].ID = "second-recovery-old-epoch-usage"
	oldOne[0].PrincipalKey = freshPlain
	if err := h.projectZeroNodeEvents(t.Context(), oldOne); err != nil {
		t.Fatal(err)
	}
	var closedOne model.ClosedEntitlement
	if err := h.db.Where("subscription_id = ? AND epoch = ?", f.subscription, 1).Take(&closedOne).Error; err != nil || closedOne.ClosingFlowUsed != 70 || closedOne.FlowUsed != 100 || closedOne.TransitionOrderID != secondRecovery.ID {
		t.Fatal("late epoch1 did not retain its own quota", err)
	}
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.FlowUsed != 70 {
		t.Fatal("second period consumed older flow", err)
	}
	if err := datastore.RunMigrations(h.db); err != nil {
		t.Fatal("valid multiple closed epochs failed recovery", err)
	}
	if err := h.db.Where("subscription_id = ? AND epoch = ?", f.subscription, 0).Delete(&model.ClosedEntitlement{}).Error; err != nil {
		t.Fatal(err)
	}
	unknown := accountingBenchmarkEvents(old, 2, 1)
	unknown[0].ID = "old-after-lost-quota"
	unknown[0].PrincipalKey = plain
	if err := h.projectZeroNodeEvents(t.Context(), unknown); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing epoch evidence selected current quota %v", err)
	}
	if err := h.db.First(&current, f.subscription).Error; err != nil || current.FlowUsed != 70 {
		t.Fatal("missing historical evidence consumed new quota", err)
	}
	if err := datastore.RunMigrations(h.db); err == nil {
		t.Fatal("startup accepted a lost closed-epoch source row")
	}
}
