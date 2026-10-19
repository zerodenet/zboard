package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionValueReadsVerifiedPaidFacts(t *testing.T) {
	assertSubscriptionValueRead(t, newOrderFixture(t))
}
func TestMySQLSubscriptionValueReadsVerifiedPaidFacts(t *testing.T) {
	assertSubscriptionValueRead(t, newMySQLOrderFixture(t))
}
func assertSubscriptionValueRead(t *testing.T, f orderFixture) {
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 10000)
	pay := func(order model.Order) {
		t.Helper()
		s := fundingService(f)
		if _, err := s.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	root := f.create(t, 0)
	pay(root)
	fact := readValueEvidence(t, f, root.ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil || quote.TotalMinor != 200 || quote.CurrentMinor != 200 || quote.FutureMinor != 0 || quote.Currency != "CNY" || quote.RootOrderID != root.ID || quote.Fingerprint == "" {
		t.Fatalf("actual paid projection: %+v %v", quote, err)
	}
	// Today's catalog is not the authority for money originally paid.
	priceForFunding(t, f, 9000)
	again, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil || again.TotalMinor != 200 || again.Fingerprint != quote.Fingerprint {
		t.Fatalf("catalog repriced old value: %+v %v", again, err)
	}
	other := model.User{Email: "value-reader-other@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), other.ID, fact.SubscriptionID); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("other account read value: %v", err)
	}
	// Multiple actual paid resets add only new cash to the remaining basis.
	f = prepareResetSKU(t, f)
	priceForFunding(t, f, 100)
	reset := f.create(t, fact.SubscriptionID)
	pay(reset)
	reset2 := f.create(t, fact.SubscriptionID)
	pay(reset2)
	quote, err = f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil || quote.TotalMinor != 400 {
		t.Fatalf("real resets revived consumed cash or discounted twice: %+v %v", quote, err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 130).Error; err != nil {
		t.Fatal(err)
	}
	quote, err = f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil || quote.TotalMinor != 200 {
		t.Fatalf("post-reset use: %+v %v", quote, err)
	}
	// Manual marking is retained as a business record, not invented cash proof.
	manualFixture := f
	manualFixture.skuRecord = rootSKUForValue(t, f, root.PlanSKUID)
	manual := manualFixture.paid(t, manualFixture.create(t, 0).ID)
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, manual.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("manual cash became refundable: %v", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("inactive actor read value: %v", err)
	}
}
func rootSKUForValue(t testing.TB, f orderFixture, id uint) model.PlanSKU {
	t.Helper()
	var sku model.PlanSKU
	if err := f.h.db.First(&sku, id).Error; err != nil {
		t.Fatal(err)
	}
	return sku
}

func TestSubscriptionTerminationSettlesOnceAndRevokesOnlyItsSubscription(t *testing.T) {
	assertSubscriptionTermination(t, newOrderFixture(t))
}
func TestMySQLSubscriptionTerminationSettlesOnceAndRevokesOnlyItsSubscription(t *testing.T) {
	assertSubscriptionTermination(t, newMySQLOrderFixture(t))
}
func assertSubscriptionTermination(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 5000)
	pay := func(order model.Order) {
		t.Helper()
		if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	root := f.create(t, 0)
	pay(root)
	other := f.create(t, 0)
	pay(other)
	fact := readValueEvidence(t, f, root.ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	// A pending renewal has reserved wallet funds; termination must release them.
	pending := f.create(t, fact.SubscriptionID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-owned-00001"}
	for i := 0; i < 3; i++ {
		out, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in)
		if err != nil || out.AmountMinor != 200 || out.Status != "canceled" || out.RevocationStatus != "pending_verification" || out.WalletEntryID == nil {
			t.Fatalf("settlement %+v %v", out, err)
		}
	}
	walletMatches(t, f, 3200, 0, 8) // top-up + 2 holds/captures + pending hold/release + refund
	var sub model.Subscription
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.EndReason != "canceled" || sub.FlowUsed != 80 {
		t.Fatalf("lifecycle/history %+v %v", sub, err)
	}
	page, err := f.h.services.SubscriptionQueries.Owned(t.Context(), 1, entitlements.SubscriptionQuery{ID: sub.ID, Limit: 25})
	if err != nil || len(page.Items) != 1 || page.Items[0].CanRenew || page.Items[0].RenewalUntil != nil || page.Items[0].Status != "canceled" {
		t.Fatalf("cancellation advertised recovery window %+v %v", page, err)
	}
	page, err = f.h.services.SubscriptionQueries.Owned(t.Context(), 1, entitlements.SubscriptionQuery{ID: sub.ID, Status: "expired", Limit: 25})
	if err != nil || page.Total != 0 {
		t.Fatalf("cancellation appeared as recoverable expiry %+v %v", page, err)
	}
	var creds []model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", sub.ID).Find(&creds).Error; err != nil || len(creds) != 1 || creds[0].Status != "revoked" {
		t.Fatalf("credential revoked %+v %v", creds, err)
	}

	otherFact := readValueEvidence(t, f, other.ID)
	var independent model.Subscription
	if err := f.h.db.First(&independent, otherFact.SubscriptionID).Error; err != nil || independent.Status != "active" {
		t.Fatalf("independent subscription affected %+v %v", independent, err)
	}
	var refunded model.Order
	if err := f.h.db.First(&refunded, root.ID).Error; err != nil || refunded.RefundAmount != 200 || refunded.SubscriptionEndedAt == nil {
		t.Fatalf("order evidence %+v %v", refunded, err)
	}
	var canceled model.Order
	if err := f.h.db.First(&canceled, pending.ID).Error; err != nil || canceled.Status != "canceled" {
		t.Fatalf("pending renewal %+v %v", canceled, err)
	}
	var count int64
	for table, want := range map[string]int64{"subscription_value_settlements": 1, "subscription_value_settlement_sources": 1, "order_value_budgets": 1} {
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s count %d %v", table, count, err)
		}
	}
	in.IdempotencyKey = "unsubscribe-other-intent"
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("second refund %v", err)
	}
}

func TestSubscriptionTerminationFailuresAndConcurrency(t *testing.T) {
	assertTerminationFailures(t, newOrderFixture(t))
}
func TestMySQLSubscriptionTerminationFailuresAndConcurrency(t *testing.T) {
	assertTerminationFailures(t, newMySQLOrderFixture(t))
}
func assertTerminationFailures(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 2000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	fact := readValueEvidence(t, f, order.ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	pendingCash := f.create(t, fact.SubscriptionID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pendingCash.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	provider := "unsubscribe-late-channel"
	prepareReceiptAttempt(t, f, pendingCash.ID, provider)
	var publicationBefore model.NodeConfigPublish
	if err := f.h.db.First(&publicationBefore).Error; err != nil {
		t.Fatal(err)
	}
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-failure-0001"}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 81).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in); !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("stale quota quote %v", err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	// Database abort at the final audit write must roll back money, source budget,
	// credentials, lifecycle and delivery intent together.
	sql := `CREATE TRIGGER reject_unsubscribe_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'subscription.unsubscribe' BEGIN SELECT RAISE(ABORT, 'unsubscribe audit unavailable'); END`
	if !datastore.IsSQLite(f.h.db) {
		sql = `CREATE TRIGGER reject_unsubscribe_audit BEFORE INSERT ON audit_logs FOR EACH ROW BEGIN IF NEW.action = 'subscription.unsubscribe' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'unsubscribe audit unavailable'; END IF; END`
	}
	if err := f.h.db.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in); err == nil {
		t.Fatal("failed audit committed settlement")
	}
	walletMatches(t, f, 1000, 0, 3)
	var sub model.Subscription
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
		t.Fatalf("partial lifecycle %+v %v", sub, err)
	}
	for _, table := range []string{"subscription_value_settlements", "subscription_value_settlement_sources", "order_value_budgets"} {
		var n int64
		if err := f.h.db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("partial %s=%d %v", table, n, err)
		}
	}
	var publicationAfter model.NodeConfigPublish
	if err := f.h.db.First(&publicationAfter, publicationBefore.NodeID).Error; err != nil || publicationAfter.Generation != publicationBefore.Generation {
		t.Fatalf("rolled-back delivery intent changed %+v %v", publicationAfter, err)
	}
	var stillActive int64
	if err := f.h.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", sub.ID, []string{"active", "prepared"}).Count(&stillActive).Error; err != nil || stillActive != 1 {
		t.Fatalf("rolled-back credential revocation %d %v", stillActive, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_unsubscribe_audit").Error; err != nil {
		t.Fatal(err)
	}
	token := model.SubscriptionToken{UserID: 1, SubscriptionID: &sub.ID, TokenHash: "unsubscribe-test-hash", TokenPrefix: "unsubscribe"}
	if err := f.h.db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("same-intent concurrent replay %v", err)
		}
	}
	walletMatches(t, f, 1200, 0, 4)
	if err := f.h.db.First(&token, token.ID).Error; err != nil || token.RevokedAt == nil {
		t.Fatalf("token still active %+v %v", token, err)
	}
	var budget model.OrderValueBudget
	if err := f.h.db.First(&budget, order.ID).Error; err != nil || budget.FundingMinor != 1000 || budget.RefundedMinor != 200 || budget.ConvertedMinor != 0 {
		t.Fatalf("source budget %+v %v", budget, err)
	}
	late := commerce.PaymentFact{OrderID: pendingCash.ID, ProviderEventID: "unsubscribe-late-event", ProviderTradeNo: "unsubscribe-late-trade", Status: "paid", AmountMinor: 1000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	for i := 0; i < 2; i++ {
		out, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, late)
		if err != nil || out.Disposition != "wallet_credit" || out.Fulfilled {
			t.Fatalf("late renewal revived canceled rights %+v %v", out, err)
		}
	}
	walletMatches(t, f, 2200, 0, 5)
	in.QuoteFingerprint = strings.Repeat("a", 64)
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
		t.Fatalf("changed replay %v", err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	in.QuoteFingerprint = quote.Fingerprint
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in); !errors.Is(err, commerce.ErrOrderPermission) {
		t.Fatalf("disabled actor replay %v", err)
	}
}

func TestSubscriptionValueSchemaRecoveryPreservesFinancialEvidence(t *testing.T) {
	assertSubscriptionValueSchema(t, newOrderFixture(t))
}
func TestMySQLSubscriptionValueSchemaRecoveryPreservesFinancialEvidence(t *testing.T) {
	assertSubscriptionValueSchema(t, newMySQLOrderFixture(t))
}
func assertSubscriptionValueSchema(t *testing.T, f orderFixture) {
	const version = "0043_subscription_value_settlements.up.sql"
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.db.Migrator().DropTable(&model.SubscriptionValueSettlementSource{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("recorded missing settlement table silently recreated")
	}
	if f.h.db.Migrator().HasTable(&model.SubscriptionValueSettlementSource{}) {
		t.Fatal("financial loss replaced by empty table")
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatalf("empty interrupted DDL not recovered %v", err)
	}
	order := f.create(t, 0)
	budget := model.OrderValueBudget{OrderID: order.ID, UserID: 1, Currency: "CNY", FundingMinor: 1000, FundingFingerprint: strings.Repeat("a", 64), Revision: 1}
	if err := f.h.db.Create(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Migrator().DropTable(&model.SubscriptionValueSettlementSource{}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("nonempty financial counterpart allowed reconstruction")
	}
	if f.h.db.Migrator().HasTable(&model.SubscriptionValueSettlementSource{}) {
		t.Fatal("missing counterpart created despite financial evidence")
	}
	var retained model.OrderValueBudget
	if err := f.h.db.First(&retained, order.ID).Error; err != nil || retained.FundingFingerprint != budget.FundingFingerprint || retained.FundingMinor != 1000 {
		t.Fatalf("financial evidence changed %+v %v", retained, err)
	}
}

func TestSubscriptionValueHTTPDoesNotAcceptMoneyOrBypassAuthentication(t *testing.T) {
	f := newOrderFixture(t)
	for _, tc := range []struct {
		method, token, body string
		want                int
	}{{"GET", "", "", 401}, {"POST", f.token, `{"quote_fingerprint":"` + strings.Repeat("a", 64) + `","idempotency_key":"unsubscribe-http-1","amount_minor":9000}`, 400}, {"GET", f.token, "", 404}} {
		out := httptest.NewRecorder()
		request := announcementRequest(tc.method, "/api/v1/subscriptions/999999/unsubscribe", tc.token, tc.body)
		f.h.SubscriptionValueHandler(out, request)
		if out.Code != tc.want || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("HTTP authorization/money input %d %s", out.Code, out.Body.String())
		}
	}
}

func TestSubscriptionTerminationRacesVerifiedRenewal(t *testing.T) {
	assertTerminationRenewalRace(t, newOrderFixture(t))
}
func TestMySQLSubscriptionTerminationRacesVerifiedRenewal(t *testing.T) {
	assertTerminationRenewalRace(t, newMySQLOrderFixture(t))
}
func assertTerminationRenewalRace(t *testing.T, f orderFixture) {
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 2000)
	root := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	fact := readValueEvidence(t, f, root.ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	pending := f.create(t, fact.SubscriptionID)
	provider := "unsubscribe-race-channel"
	prepareReceiptAttempt(t, f, pending.ID, provider)
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-race-request"}
	cash := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "unsubscribe-race-event", ProviderTradeNo: "unsubscribe-race-trade", Status: "paid", AmountMinor: 1000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	start := make(chan struct{})
	done := make(chan error, 1)
	paid := make(chan error, 1)
	go func() {
		<-start
		_, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in)
		done <- err
	}()
	go func() {
		<-start
		_, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, cash)
		paid <- err
	}()
	close(start)
	cancelErr, payErr := <-done, <-paid
	if payErr != nil {
		t.Fatalf("verified race payment %v", payErr)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, fact.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if cancelErr == nil {
		if sub.Status != "canceled" || sub.EndedAt == nil {
			t.Fatalf("cancellation lost %+v", sub)
		}
		walletMatches(t, f, 2200, 0, 5)
	} else if errors.Is(cancelErr, commerce.ErrSubscriptionQuoteChanged) {
		if sub.Status != "active" || !sub.EndAt.After(fact.After.EndAt) {
			t.Fatalf("paid renewal not retained %+v", sub)
		}
		walletMatches(t, f, 1000, 0, 3)
	} else {
		t.Fatalf("unhandled race result %v", cancelErr)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements > 1 {
		t.Fatalf("duplicate race settlement %d %v", settlements, err)
	}
}

func TestMySQLSubscriptionTerminationFencesCheckoutInsertionAtReadCommitted(t *testing.T) {
	f := newMySQLOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 2}).Error; err != nil {
		t.Fatal(err)
	}
	priceForFunding(t, f, 1000)
	fundTestWallet(t, f, 2000)
	root := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	fact := readValueEvidence(t, f, root.ID)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = ?", fact.SubscriptionID).Update("flow_used", 80).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, fact.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	const hook = "test:checkout_before_unsubscribe_account_lock"
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" || !injected.CompareAndSwap(false, true) {
			return
		}
		order := f.create(t, fact.SubscriptionID)
		if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.h.db.Callback().Query().Remove(hook)
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "unsubscribe-insertion-fence"}
	err = f.h.db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED").Error; err != nil {
			return err
		}
		defer conn.Exec("SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ")
		service := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: conn}}
		_, err := service.Terminate(t.Context(), 1, fact.SubscriptionID, in)
		return err
	})
	if !injected.Load() || !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("concurrent checkout escaped fence %v injected=%t", err, injected.Load())
	}
	walletMatches(t, f, 1000, 1000, 4)
	var count int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial insertion-race settlement %d %v", count, err)
	}
	if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, fact.SubscriptionID, in); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 1200, 0, 6)
}
