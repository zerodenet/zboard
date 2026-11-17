package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdministrativeUnpricedZeroTermination(t *testing.T) {
	assertUnpricedZeroTermination(t, newOrderFixture(t))
}
func TestMySQLAdministrativeUnpricedZeroTermination(t *testing.T) {
	assertUnpricedZeroTermination(t, newMySQLBusinessOrderFixture(t))
}
func assertUnpricedZeroTermination(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 4000)
	root := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	fact := readValueEvidence(t, f, root.ID)
	other := f.paid(t, f.create(t, 0).ID)
	pending := f.create(t, fact.SubscriptionID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	// Retain the actual payment and order history. This legacy row has no reliable
	// entitlement allocation snapshot; zero termination must not invent one.
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("entitlement_value_snapshot", "").Error; err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "unpriced-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	ordinary := model.User{Email: "unpriced-ordinary@example.test", Password: "unused", Status: "active"}
	for _, u := range []*model.User{&admin, &ordinary} {
		if err := f.h.db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	token := model.SubscriptionToken{UserID: 1, SubscriptionID: &fact.SubscriptionID, TokenHash: strings.Repeat("a", 64), TokenPrefix: "legacy-zero"}
	if err := f.h.db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	consent := model.SubscriptionAutomaticRenewal{SubscriptionID: fact.SubscriptionID, UserID: 1, Enabled: true, PlanSKUID: f.skuRecord.ID, RootOrderID: root.ID, AgreementDigest: strings.Repeat("a", 64), State: "enabled", NextAttemptAt: time.Now().UTC()}
	if err := f.h.db.Create(&consent).Error; err != nil {
		t.Fatal(err)
	}
	var before model.NodeConfigPublish
	if err := f.h.db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), admin.ID, fact.SubscriptionID)
	if err != nil || q.ValuationStatus != "unavailable" || q.TotalMinor != nil || q.CurrentMinor != nil || q.FutureMinor != nil || q.RootOrderID != nil || q.Currency != "" || q.MaxRefundMinor != 0 {
		t.Fatalf("unknown quote %+v %v", q, err)
	}
	raw, _ := json.Marshal(q)
	if !strings.Contains(string(raw), `"total_minor":null`) || strings.Contains(string(raw), `"root_order_id"`) {
		t.Fatalf("unknown represented as payment: %s", raw)
	}
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("ordinary valuation %v", err)
	}
	zero := int64(0)
	in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "unpriced-explicit-zero"}, AmountMinor: &zero, Reason: strings.Repeat("r", 400)}
	bad := in
	bad.AmountMinor = nil
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, fact.SubscriptionID, bad); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("implicit zero %v", err)
	}
	positive := int64(1)
	bad.AmountMinor = &positive
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, fact.SubscriptionID, bad); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("unknown positive %v", err)
	}
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), ordinary.ID, fact.SubscriptionID, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("ordinary approval %v", err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, fact.SubscriptionID, in); !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("stale usage %v", err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 0).Error; err != nil {
		t.Fatal(err)
	}
	// Fail the last durable evidence write, after lifecycle/audit/publication work.
	trigger := `CREATE TRIGGER reject_unpriced_evidence BEFORE UPDATE ON quota_events WHEN NEW.reference_type = 'subscription_mutation' BEGIN SELECT RAISE(ABORT, 'evidence unavailable'); END`
	if !datastore.IsSQLite(f.h.db) {
		trigger = `CREATE TRIGGER reject_unpriced_evidence BEFORE UPDATE ON quota_events FOR EACH ROW BEGIN IF NEW.reference_type = 'subscription_mutation' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'evidence unavailable'; END IF; END`
	}
	if err := f.h.db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, fact.SubscriptionID, in); err == nil {
		t.Fatal("missing evidence committed")
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
		t.Fatalf("partial rights %+v %v", sub, err)
	}
	walletMatches(t, f, 3000, 1000, 4)
	var after model.NodeConfigPublish
	if err := f.h.db.First(&after, before.NodeID).Error; err != nil || after.Generation != before.Generation {
		t.Fatalf("partial publication %+v %v", after, err)
	}
	for _, table := range []string{"subscription_mutations", "subscription_value_settlements", "order_value_budgets"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("partial %s %d %v", table, n, err)
		}
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_unpriced_evidence").Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	receipts := make(chan commerce.SubscriptionTerminationReceipt, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, fact.SubscriptionID, in)
			receipts <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(receipts)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent zero %v", err)
		}
	}
	var receiptID uint
	for r := range receipts {
		if r.ID == 0 || receiptID != 0 && receiptID != r.ID || r.RecordKind != "entitlement_mutation" || r.AmountMinor != 0 || r.WalletEntryID != nil || r.Currency != "" || r.Status != "canceled" {
			t.Fatalf("unpriced receipt %+v", r)
		}
		receiptID = r.ID
	}
	walletMatches(t, f, 3000, 0, 5)
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.EndReason != "canceled" {
		t.Fatalf("rights %+v %v", sub, err)
	}
	if err := f.h.db.First(&token, token.ID).Error; err != nil || token.RevokedAt == nil {
		t.Fatalf("token %+v %v", token, err)
	}
	if err := f.h.db.First(&consent, fact.SubscriptionID).Error; err != nil || consent.Enabled {
		t.Fatalf("automatic renewal %+v %v", consent, err)
	}
	var active int64
	if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status <> ?", sub.ID, "revoked").Count(&active).Error; err != nil || active != 0 {
		t.Fatalf("credentials %d %v", active, err)
	}
	if err := f.h.db.First(&after, before.NodeID).Error; err != nil || after.Generation <= before.Generation {
		t.Fatalf("missing delivery intent %+v %v", after, err)
	}
	var otherSub model.Subscription
	if err := f.h.db.First(&otherSub, other.SubscriptionID).Error; err != nil || otherSub.Status != "active" {
		t.Fatalf("other subscription %+v %v", otherSub, err)
	}
	for _, table := range []string{"subscription_value_settlements", "subscription_value_settlement_sources", "order_value_budgets"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("fabricated financial record %s %d %v", table, n, err)
		}
	}
	var event model.QuotaEvent
	if err := f.h.db.Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", sub.ID, "unsubscribe", "subscription_mutation", strconv.FormatUint(uint64(receiptID), 10)).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Reason      string
		ActorID     uint   `json:"actor_id"`
		ActualMinor string `json:"actual_minor"`
		Quote       commerce.AdministrativeSubscriptionValueQuote
	}
	if err := json.Unmarshal([]byte(event.Detail), &evidence); err != nil || evidence.Reason != in.Reason || evidence.ActorID != admin.ID || evidence.ActualMinor != "0" || evidence.Quote.TotalMinor != nil {
		t.Fatalf("incomplete approval %s %v", event.Detail, err)
	}
	var audit model.AuditLog
	if err := f.h.db.Where("action = ? AND target = ?", "subscription.unsubscribe", fmt.Sprintf("subscription:%d", sub.ID)).First(&audit).Error; err != nil || audit.UserID == nil || *audit.UserID != admin.ID || audit.Actor != admin.Email || !strings.Contains(audit.Detail, "default_minor=unknown actual_minor=0") || !strings.Contains(audit.Detail, in.Reason) {
		t.Fatalf("approval audit %+v %v", audit, err)
	}
	bad = in
	bad.Reason = "changed reason"
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, sub.ID, bad); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("changed replay %v", err)
	}
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, otherSub.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("cross-subscription key %v", err)
	}
	if err := f.h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), admin.ID, sub.ID, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("revoked replay %v", err)
	}
}

func TestAdministrativeUnpricedZeroWithoutAnyOrder(t *testing.T) {
	assertUnpricedNoOrder(t, newOrderFixture(t))
}
func TestMySQLAdministrativeUnpricedZeroWithoutAnyOrder(t *testing.T) {
	assertUnpricedNoOrder(t, newMySQLBusinessOrderFixture(t))
}
func assertUnpricedNoOrder(t *testing.T, f orderFixture) {
	t.Helper()
	sub := model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", FlowTotal: 100, StartAt: time.Now().UTC(), EndAt: time.Now().UTC().Add(24 * time.Hour), Config: "{}"}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	queryFailure := errors.New("injected durable order read failure")
	callbackName := "unpriced_zero_order_read_failure"
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "orders" {
			db.AddError(queryFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, readErr := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, sub.ID)
	if err := f.h.db.Callback().Query().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(readErr, queryFailure) {
		t.Fatalf("database read failure became an unavailable quote: %v", readErr)
	}
	q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, sub.ID)
	if err != nil || q.ValuationStatus != "unavailable" {
		t.Fatalf("no-order quote %+v %v", q, err)
	}
	zero := int64(0)
	in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "no-payment-zero-stop"}, AmountMinor: &zero, Reason: "legacy rights stopped without inventing payment"}
	r, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, sub.ID, in)
	if err != nil || r.RecordKind != "entitlement_mutation" {
		t.Fatalf("no-order stop %+v %v", r, err)
	}
	var count int64
	if err := f.h.db.Model(&model.Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("fabricated order %d %v", count, err)
	}
	if err := f.h.db.Model(&model.WalletEntry{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("fabricated money %d %v", count, err)
	}
}

func TestAdministrativeUnpricedZeroPendingCycleAndMissingFulfillment(t *testing.T) {
	assertUnpricedIncompleteEvidence(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLAdministrativeUnpricedZeroPendingCycleAndMissingFulfillment(t *testing.T) {
	assertUnpricedIncompleteEvidence(t, newMySQLBusinessOrderFixture)
}
func assertUnpricedIncompleteEvidence(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			f, root := perpetualValueFixtureWithBase(t, fixture(t), 20)
			if pending {
				past := time.Now().UTC().Add(-time.Hour)
				if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", root.SubscriptionID).Updates(map[string]any{"next_reset_at": past, "end_at": time.Now().UTC().Add(time.Hour)}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("fulfilled_at", nil).Error; err != nil {
					t.Fatal(err)
				}
			}
			q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, root.SubscriptionID)
			if err != nil || q.ValuationStatus != "unavailable" || q.TotalMinor != nil {
				t.Fatalf("unverified source quote %+v %v", q, err)
			}
			zero := int64(0)
			in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "incomplete-zero-termination"}, AmountMinor: &zero, Reason: "explicit zero despite unavailable historical residual"}
			r, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, root.SubscriptionID, in)
			if err != nil || r.RecordKind != "entitlement_mutation" || r.WalletEntryID != nil {
				t.Fatalf("unverified source stop %+v %v", r, err)
			}
		})
	}
}
