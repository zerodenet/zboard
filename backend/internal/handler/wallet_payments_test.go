package handler

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func fundTestWallet(t testing.TB, f orderFixture, amount int64) {
	t.Helper()
	topup := createTopUp(t, f, amount, fmt.Sprintf("wallet-source-%d", amount))
	provider := "test-wallet-source"
	prepareReceiptAttempt(t, f, topup.ID, provider)
	fact := commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: fmt.Sprintf("topup-event-%d", topup.ID), ProviderTradeNo: fmt.Sprintf("topup-trade-%d", topup.ID), Status: "paid", AmountMinor: amount, Currency: "CNY", OccurredAt: time.Now().UTC()}
	if _, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}, fact); err != nil {
		t.Fatal(err)
	}
}
func fundingService(f orderFixture) commerce.WalletPayments {
	return f.h.services.WalletPayments(f.h.credentialCipher, f.h.zeroMieruAccess)
}
func walletMatches(t testing.TB, f orderFixture, balance, reserved int64, entries int) {
	t.Helper()
	wallet, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 100)
	if err != nil || wallet.BalanceMinor != balance || wallet.ReservedMinor != reserved || wallet.AvailableMinor != balance-reserved || len(wallet.Entries) != entries {
		t.Fatalf("wallet balance/reservation invariant: %+v %v", wallet, err)
	}
	var sums struct{ Balance, Reserved int64 }
	if err := f.h.db.Model(&model.WalletEntry{}).Where("user_id = ? AND currency = ?", 1, "CNY").Select("COALESCE(SUM(balance_delta),0) AS balance, COALESCE(SUM(reserved_delta),0) AS reserved").Scan(&sums).Error; err != nil || sums.Balance != balance || sums.Reserved != reserved {
		t.Fatalf("ledger/account divergence: %+v %v", sums, err)
	}
}
func priceForFunding(t testing.TB, f orderFixture, amount int64) {
	t.Helper()
	if err := f.h.db.Model(&f.skuRecord).Update("price_cents", amount).Error; err != nil {
		t.Fatal(err)
	}
}
func TestWalletMixedPaymentMatchesFrozenCashAndCapturesOnce(t *testing.T) {
	assertMixedFunding(t, newOrderFixture(t))
}
func TestMySQLWalletMixedPaymentMatchesFrozenCashAndCapturesOnce(t *testing.T) {
	assertMixedFunding(t, newMySQLBusinessOrderFixture(t))
}
func assertMixedFunding(t *testing.T, f orderFixture) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 3000)
	order := f.create(t, 0)
	service := fundingService(f)
	for i := 0; i < 2; i++ {
		view, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true})
		if err != nil || view.WalletMinor != 3000 || view.CashMinor != 2000 || view.AmountMinor != 5000 || view.AvailableMinor != 0 {
			t.Fatalf("frozen split: %+v %v", view, err)
		}
	}
	walletMatches(t, f, 3000, 3000, 2)
	if _, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: false}); !errors.Is(err, commerce.ErrFundingConflict) {
		t.Fatalf("changed split accepted: %v", err)
	}
	if _, err := service.Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("wallet payment bypassed required cash")
	}
	if out := f.pay(t, order.ID, false); out.Code != 400 {
		t.Fatalf("manual state bypassed mixed cash: %d %s", out.Code, out.Body.String())
	}
	provider := "test-mixed-channel"
	context, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, order.ID, provider)
	if err != nil || !permitted || context.PayableAmount != 2000 {
		t.Fatalf("plugin received full amount instead of supplement: %+v %v %v", context, permitted, err)
	}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "mixed-first", ProviderTradeNo: "mixed-trade", Status: "paid", AmountMinor: 5000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	facts := receiptService(f)
	if _, err := facts.Record(t.Context(), authority, fact); !errors.Is(err, commerce.ErrPaymentFactConflict) {
		t.Fatalf("wrong cash amount accepted: %v", err)
	}
	fact.AmountMinor = 2000
	for i := 0; i < 3; i++ {
		if i == 2 {
			fact.ProviderEventID = "mixed-new-event-same-cash"
		}
		out, err := facts.Record(t.Context(), authority, fact)
		if err != nil || out.Status != "paid" || (i == 0 && !out.Fulfilled) || (i > 0 && out.Fulfilled) {
			t.Fatalf("mixed settlement: %+v %v", out, err)
		}
	}
	walletMatches(t, f, 0, 0, 3)
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.PaidAmount != 5000 || actual.Status != "paid" {
		t.Fatalf("total paid lost wallet portion: %+v %v", actual, err)
	}
	var fund model.OrderFunding
	if err := f.h.db.First(&fund, order.ID).Error; err != nil || fund.State != "captured" || fund.WalletMinor != 3000 || fund.CashMinor != 2000 {
		t.Fatalf("capture receipt: %+v %v", fund, err)
	}
	fact.ProviderEventID, fact.ProviderTradeNo = "mixed-second-cash", "mixed-second-trade"
	if out, err := facts.Record(t.Context(), authority, fact); err != nil || out.Disposition != "wallet_credit" || out.Fulfilled {
		t.Fatalf("second supplement lost: %+v %v", out, err)
	}
	walletMatches(t, f, 2000, 0, 4)
	for table, want := range map[string]int64{"subscriptions": 1, "protocol_credentials": 1, "quota_events": 1, "node_config_publishes": 1} {
		var count int64
		if err := f.h.db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s duplicated: %d %v", table, count, err)
		}
	}
}
func TestWalletReservationsReleaseBeforeLateCash(t *testing.T) {
	for _, reason := range []string{"canceled", "failed", "expired", "expired_without_worker"} {
		t.Run(reason, func(t *testing.T) { assertFundingRelease(t, newOrderFixture(t), reason) })
	}
}
func TestMySQLWalletReservationsReleaseBeforeLateCash(t *testing.T) {
	for _, reason := range []string{"canceled", "failed", "expired", "expired_without_worker"} {
		t.Run(reason, func(t *testing.T) { assertFundingRelease(t, newMySQLOrderFixture(t), reason) })
	}
}
func assertFundingRelease(t *testing.T, f orderFixture, reason string) {
	t.Helper()
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 3000)
	order := f.create(t, 0)
	service := fundingService(f)
	if _, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	provider := "release-channel"
	prepareReceiptAttempt(t, f, order.ID, provider)
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: provider, RequireAttempt: true}
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "released-paid", ProviderTradeNo: "released-trade", Status: "paid", AmountMinor: 2000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	switch reason {
	case "canceled":
		if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	case "failed":
		failed := fact
		failed.Status = "failed"
		failed.ProviderEventID = "released-failed"
		if _, err := receiptService(f).Record(t.Context(), authority, failed); err != nil {
			t.Fatal(err)
		}
	default:
		if err := f.h.db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		if reason == "expired" {
			count, err := service.Expire(t.Context(), time.Now().UTC(), 200)
			if err != nil || count != 1 {
				t.Fatalf("expiry: %d %v", count, err)
			}
			count, err = service.Expire(t.Context(), time.Now().UTC(), 200)
			if err != nil || count != 0 {
				t.Fatalf("expiry replay: %d %v", count, err)
			}
		}
	}
	if reason != "expired_without_worker" {
		walletMatches(t, f, 3000, 0, 3)
	}
	for i := 0; i < 2; i++ {
		out, err := receiptService(f).Record(t.Context(), authority, fact)
		if err != nil || out.Status != "canceled" || out.Fulfilled || out.Disposition != "wallet_credit" {
			t.Fatalf("late supplement: %+v %v", out, err)
		}
	}
	walletMatches(t, f, 5000, 0, 4)
	var count int64
	if err := f.h.db.Model(&model.Subscription{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("late payment revived service: %d %v", count, err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 5000, 0, 4)
}
func TestWalletOnlyPaymentRollsBackAndRacesWithoutDoubleSpend(t *testing.T) {
	assertWalletOnly(t, newOrderFixture(t), false)
}
func TestMySQLWalletOnlyPaymentRollsBackAndRacesWithoutDoubleSpend(t *testing.T) {
	assertWalletOnly(t, newMySQLBusinessOrderFixture(t), true)
}
func assertWalletOnly(t *testing.T, f orderFixture, mysql bool) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 6000)
	order := f.create(t, 0)
	service := fundingService(f)
	if _, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if mysql {
		if err := f.h.db.Exec("CREATE TRIGGER reject_wallet_publish BEFORE INSERT ON node_config_publishes FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'outbox unavailable'").Error; err != nil {
			t.Fatal(err)
		}
	} else {
		if err := f.h.db.Exec("CREATE TRIGGER reject_wallet_publish BEFORE INSERT ON node_config_publishes BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END").Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("publication failure acknowledged wallet spend")
	}
	walletMatches(t, f, 6000, 5000, 2)
	var pending model.Order
	if err := f.h.db.First(&pending, order.ID).Error; err != nil || pending.Status != "pending" {
		t.Fatalf("partial funded order: %+v %v", pending, err)
	}
	if err := f.h.db.Exec("DROP TRIGGER reject_wallet_publish").Error; err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	type attempt struct {
		out commerce.WalletPaymentResult
		err error
	}
	results := make(chan attempt, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			out, err := fundingService(f).Pay(t.Context(), 1, order.ID)
			results <- attempt{out, err}
		}()
	}
	group.Wait()
	close(results)
	fulfilled := 0
	for result := range results {
		if result.err != nil || result.out.Status != "paid" {
			t.Fatalf("wallet race: %+v %v", result.out, result.err)
		}
		if result.out.Fulfilled {
			fulfilled++
		}
	}
	if fulfilled != 1 {
		t.Fatalf("duplicate grants: %d", fulfilled)
	}
	walletMatches(t, f, 1000, 0, 3)
	if _, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, order.ID, "new-cash-after-wallet"); err != nil || permitted {
		t.Fatalf("wallet-paid order opened channel collection: %v %v", permitted, err)
	}
}
func TestWalletFundingRefusesChangesAfterChannelAdmissionAndSpoofedAmounts(t *testing.T) {
	f := newOrderFixture(t)
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 6000)
	order := f.create(t, 0)
	prepareReceiptAttempt(t, f, order.ID, "already-prepared")
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err == nil {
		t.Fatal("changed cash after channel admission")
	}
	walletMatches(t, f, 6000, 0, 1)
	for _, target := range []struct{ path, body string }{{"funding", `{"use_balance":true,"cash_minor":0}`}, {"wallet-pay", `{"paid_amount":0}`}} {
		out := httptest.NewRecorder()
		request := announcementRequest("POST", fmt.Sprintf("/api/v1/orders/%d/%s", order.ID, target.path), f.token, target.body)
		if target.path == "funding" {
			f.h.OrderFundingHandler(out, request)
		} else {
			f.h.WalletPayOrderHandler(out, request)
		}
		if out.Code != 400 {
			t.Fatalf("financial input spoof: %d %s", out.Code, out.Body.String())
		}
	}
}
func TestWalletFreeOrdersNeedExplicitOwnerConfirmation(t *testing.T) {
	f := newOrderFixture(t)
	priceForFunding(t, f, 0)
	order := f.create(t, 0)
	service := fundingService(f)
	view, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{})
	if err != nil || view.CashMinor != 0 || view.WalletMinor != 0 {
		t.Fatalf("free split: %+v %v", view, err)
	}
	var count int64
	f.h.db.Model(&model.Subscription{}).Count(&count)
	if count != 0 {
		t.Fatal("configuration auto fulfilled free order")
	}
	out, err := service.Pay(t.Context(), 1, order.ID)
	if err != nil || !out.Fulfilled || out.Status != "paid" {
		t.Fatalf("confirmed free sale: %+v %v", out, err)
	}
	walletMatches(t, f, 0, 0, 0)
}

func TestWalletReservationCompetitionCannotOverspend(t *testing.T) {
	assertFundingCompetition(t, newOrderFixture(t))
}
func TestMySQLWalletReservationCompetitionCannotOverspend(t *testing.T) {
	assertFundingCompetition(t, newMySQLBusinessOrderFixture(t))
}
func assertFundingCompetition(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 5000)
	orders := []model.Order{f.create(t, 0), f.create(t, 0)}
	type result struct {
		view commerce.OrderFundingView
		err  error
	}
	results := make(chan result, 2)
	var group sync.WaitGroup
	for _, order := range orders {
		group.Add(1)
		go func(id uint) {
			defer group.Done()
			view, err := fundingService(f).Configure(t.Context(), 1, id, commerce.FundingRequest{UseBalance: true})
			results <- result{view, err}
		}(order.ID)
	}
	group.Wait()
	close(results)
	reserved := int64(0)
	var winner uint
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		reserved += result.view.WalletMinor
		if result.view.CashMinor == 0 {
			winner = result.view.OrderID
		}
	}
	if reserved != 5000 || winner == 0 {
		t.Fatalf("non-atomic competing reservation: %d winner:%d", reserved, winner)
	}
	walletMatches(t, f, 5000, 5000, 2)
	if _, err := fundingService(f).Pay(t.Context(), 1, winner); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 0, 0, 3)
}
func TestWalletPaymentCancellationRaceHasOneFinancialOutcome(t *testing.T) {
	assertFundingCancelRace(t, newOrderFixture(t))
}
func TestMySQLWalletPaymentCancellationRaceHasOneFinancialOutcome(t *testing.T) {
	assertFundingCancelRace(t, newMySQLBusinessOrderFixture(t))
}
func assertFundingCancelRace(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 5000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	var payErr, cancelErr error
	group.Add(2)
	go func() { defer group.Done(); <-start; _, payErr = fundingService(f).Pay(t.Context(), 1, order.ID) }()
	go func() {
		defer group.Done()
		<-start
		_, cancelErr = f.h.services.OrderCancellation.Owned(t.Context(), 1, order.ID)
	}()
	close(start)
	group.Wait()
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	switch actual.Status {
	case "paid":
		if payErr != nil || cancelErr == nil {
			t.Fatalf("paid race errors: %v %v", payErr, cancelErr)
		}
		walletMatches(t, f, 0, 0, 3)
	case "canceled":
		if cancelErr != nil || payErr == nil {
			t.Fatalf("canceled race errors: %v %v", payErr, cancelErr)
		}
		walletMatches(t, f, 5000, 0, 3)
	default:
		t.Fatalf("race has no durable outcome: %+v %v %v", actual, payErr, cancelErr)
	}
}
func TestWalletFundingRetainsLegacyWholeCashWithoutCapturingBalance(t *testing.T) {
	assertLegacyWholeFunding(t, newOrderFixture(t))
}
func TestMySQLWalletFundingRetainsLegacyWholeCashWithoutCapturingBalance(t *testing.T) {
	assertLegacyWholeFunding(t, newMySQLBusinessOrderFixture(t))
}
func assertLegacyWholeFunding(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 3000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	// A trusted old integration may have already collected the original full
	// quote, without a host attempt. Preserve that actual cash rather than
	// rejecting it because the newer split expected only a 2000 supplement.
	fact := commerce.PaymentFact{OrderID: order.ID, ProviderEventID: "old-integration-whole", ProviderTradeNo: "old-integration-whole-trade", Status: "paid", AmountMinor: 5000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "integration:old", RequireAdministrator: true}
	for i := 0; i < 2; i++ {
		out, err := receiptService(f).Record(t.Context(), authority, fact)
		if err != nil || out.Status != "canceled" || out.Disposition != "wallet_credit" || out.Fulfilled {
			t.Fatalf("old full cash lost or captured wallet: %+v %v", out, err)
		}
	}
	walletMatches(t, f, 8000, 0, 4)
}
func TestWalletFundingMigrationRetainsReservationsAndRefusesMissingEvidence(t *testing.T) {
	assertFundingSchema(t, newOrderFixture(t))
}
func TestMySQLWalletFundingMigrationRetainsReservationsAndRefusesMissingEvidence(t *testing.T) {
	assertFundingSchema(t, newMySQLOrderFixture(t))
}
func assertFundingSchema(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 5000)
	fundTestWallet(t, f, 3000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	walletMatches(t, f, 3000, 3000, 2)
	var fund model.OrderFunding
	if err := f.h.db.First(&fund, order.ID).Error; err != nil || fund.State != "reserved" || fund.CashMinor != 2000 {
		t.Fatalf("migration rewrote reservation: %+v %v", fund, err)
	}
	if err := f.h.db.Exec("DROP TABLE order_fundings").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil || f.h.db.Migrator().HasTable("order_fundings") {
		t.Fatalf("lost reservation journal replaced with empty state: %v", err)
	}
	walletMatches(t, f, 3000, 3000, 2)
}
func TestWalletFundingCompletesUnrecordedDDLAndPreservesOrders(t *testing.T) {
	assertFundingUnrecorded(t, newOrderFixture(t))
}
func TestMySQLWalletFundingCompletesUnrecordedDDLAndPreservesOrders(t *testing.T) {
	assertFundingUnrecorded(t, newMySQLOrderFixture(t))
}
func assertFundingUnrecorded(t *testing.T, f orderFixture) {
	t.Helper()
	order := f.create(t, 0)
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0040_order_funding.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal(err)
	}
	var actual model.Order
	if err := f.h.db.First(&actual, order.ID).Error; err != nil || actual.PayableAmount != order.PayableAmount || actual.Status != "pending" {
		t.Fatalf("migration changed an old pending order: %+v %v", actual, err)
	}
	column := "CashMinor"
	if datastore.IsSQLite(f.h.db) {
		column = "UseBalance"
	}
	if err := f.h.db.Migrator().DropColumn(&model.OrderFunding{}, column); err != nil {
		// MySQL correctly refuses dropping a column referenced by a CHECK. Remove
		// only that isolated test constraint to inject the missing evidence.
		if datastore.IsSQLite(f.h.db) {
			t.Fatal(err)
		}
		if err := f.h.db.Exec("ALTER TABLE order_fundings DROP CHECK ck_order_funding_amounts").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.h.db.Migrator().DropColumn(&model.OrderFunding{}, "CashMinor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := datastore.RunMigrations(f.h.db); err == nil || f.h.db.Migrator().HasColumn(&model.OrderFunding{}, column) {
		t.Fatalf("missing funding split was fabricated: %v", err)
	}
}

func TestWalletFundingKeepsOwnerScopeAndCannotRechargeItself(t *testing.T) {
	assertFundingOwner(t, newOrderFixture(t))
}
func TestMySQLWalletFundingKeepsOwnerScopeAndCannotRechargeItself(t *testing.T) {
	assertFundingOwner(t, newMySQLBusinessOrderFixture(t))
}
func assertFundingOwner(t *testing.T, f orderFixture) {
	t.Helper()
	fundTestWallet(t, f, 1000)
	order := f.create(t, 0)
	outsider := model.User{Email: "funding-outsider@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&outsider).Error; err != nil {
		t.Fatal(err)
	}
	service := fundingService(f)
	if _, err := service.Read(t.Context(), outsider.ID, order.ID); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign funding read: %v", err)
	}
	if _, err := service.Configure(t.Context(), outsider.ID, order.ID, commerce.FundingRequest{UseBalance: true}); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign reservation: %v", err)
	}
	if _, err := service.Pay(t.Context(), outsider.ID, order.ID); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign wallet payment: %v", err)
	}
	topup := createTopUp(t, f, 1200, "wallet-cannot-recharge")
	if _, err := service.Configure(t.Context(), 1, topup.ID, commerce.FundingRequest{UseBalance: true}); err == nil {
		t.Fatal("wallet funded its own topup")
	}
	walletMatches(t, f, 1000, 0, 1)
}

func TestWalletFundingRefusesLostAccountWithExistingLedger(t *testing.T) {
	assertFundingLostAccount(t, newOrderFixture(t))
}
func TestMySQLWalletFundingRefusesLostAccountWithExistingLedger(t *testing.T) {
	assertFundingLostAccount(t, newMySQLOrderFixture(t))
}
func assertFundingLostAccount(t *testing.T, f orderFixture) {
	t.Helper()
	fundTestWallet(t, f, 1000)
	order := f.create(t, 0)
	// Explicit corruption injection on one pinned connection in a random test
	// database. Production always retains both financial foreign keys.
	if err := f.h.db.Connection(func(tx *gorm.DB) error {
		off, on := "SET FOREIGN_KEY_CHECKS=0", "SET FOREIGN_KEY_CHECKS=1"
		if datastore.IsSQLite(tx) {
			off, on = "PRAGMA foreign_keys=OFF", "PRAGMA foreign_keys=ON"
		}
		if err := tx.Exec(off).Error; err != nil {
			return err
		}
		defer tx.Exec(on)
		return tx.Exec("DELETE FROM wallet_accounts WHERE user_id = ? AND currency = ?", 1, "CNY").Error
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Read(t.Context(), 1, order.ID); !errors.Is(err, commerce.ErrWalletUnavailable) {
		t.Fatalf("lost account appeared as zero: %v", err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); !errors.Is(err, commerce.ErrWalletUnavailable) {
		t.Fatalf("lost account changed funding to full channel price: %v", err)
	}
	topup := createTopUp(t, f, 1200, "lost-account-next-cash")
	prepareReceiptAttempt(t, f, topup.ID, "lost-account-channel")
	fact := commerce.PaymentFact{OrderID: topup.ID, ProviderEventID: "lost-account-cash", ProviderTradeNo: "lost-account-trade", Status: "paid", AmountMinor: 1200, Currency: "CNY", OccurredAt: time.Now().UTC()}
	if _, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "lost-account-channel", RequireAttempt: true}, fact); !errors.Is(err, commerce.ErrWalletUnavailable) {
		t.Fatalf("credit fabricated an empty account: %v", err)
	}
	var count int64
	if err := f.h.db.Model(&model.WalletAccount{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("lost account was recreated: %d %v", count, err)
	}
}

func TestWalletFundingNeverCombinesDifferentCurrencies(t *testing.T) {
	assertFundingCurrency(t, newOrderFixture(t))
}
func TestMySQLWalletFundingNeverCombinesDifferentCurrencies(t *testing.T) {
	assertFundingCurrency(t, newMySQLBusinessOrderFixture(t))
}
func assertFundingCurrency(t *testing.T, f orderFixture) {
	t.Helper()
	fundTestWallet(t, f, 6000)
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"currency": "USD", "price_cents": 5000}).Error; err != nil {
		t.Fatal(err)
	}
	order := f.create(t, 0)
	view, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true})
	if err != nil || view.Currency != "USD" || view.WalletMinor != 0 || view.AvailableMinor != 0 || view.CashMinor != 5000 {
		t.Fatalf("funding converted currency without agreement: %+v %v", view, err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); err == nil {
		t.Fatal("CNY funds paid a USD order")
	}
	walletMatches(t, f, 6000, 0, 1)
}
func TestWalletReservationExpiryReleasesInactiveOwnerFunds(t *testing.T) {
	f := newOrderFixture(t)
	fundTestWallet(t, f, 1000)
	order := f.create(t, 0)
	service := fundingService(f)
	if _, err := service.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.OrderFunding{}).Where("order_id = ?", order.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	count, err := service.Expire(t.Context(), time.Now().UTC(), 200)
	if err != nil || count != 1 {
		t.Fatalf("inactive owner funds remained reserved: %d %v", count, err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 1000, 0, 3)
}
