package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// A provider conformance fixture only, not external native/sandbox acceptance.
// The baseline root is genuinely wallet-paid through existing core fulfillment;
// every staged order, wallet entry, consent and redemption intent is persisted.
type stagedFiniteProvider struct {
	base         *automaticRecurringQuoter
	db           *gorm.DB
	mu           sync.Mutex
	states       map[string]commerce.DiscountRedemptionResult
	reserves     int
	finite       bool
	loseACK      bool
	offline      bool
	probe        bool
	afterReserve func()
}

func (p *stagedFiniteProvider) Quote(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	p.mu.Lock()
	offline, finite := p.offline, p.finite
	p.mu.Unlock()
	if offline {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteUnavailable
	}
	out, err := p.base.Quote(ctx, provider, in)
	if err == nil && finite {
		out.Offer.Redemption = commerce.DiscountRedemptionGuarantee
	}
	return out, err
}
func (p *stagedFiniteProvider) RedeemDiscount(ctx context.Context, in commerce.DiscountRedemptionRequest) (commerce.DiscountRedemptionResult, error) {
	if p.probe {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := p.db.WithContext(probe).Exec("UPDATE automatic_renewal_scans SET sequence=sequence WHERE id=1").Error; err != nil {
			return commerce.DiscountRedemptionResult{}, err
		}
	}
	if err := commerce.ValidateDiscountRedemptionRequest(in); err != nil {
		return commerce.DiscountRedemptionResult{}, err
	}
	p.mu.Lock()
	if p.offline {
		p.mu.Unlock()
		return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
	}
	out, ok := p.states[in.Binding.Key]
	if !ok {
		out = commerce.DiscountRedemptionResult{Version: 1, Key: in.Binding.Key, BindingHash: in.BindingHash, State: "absent", Guarantee: commerce.DiscountRedemptionGuarantee}
	}
	if out.BindingHash != in.BindingHash {
		p.mu.Unlock()
		return out, commerce.ErrDiscountRedemptionConflict
	}
	var hook func()
	lost := false
	switch in.Action {
	case "reserve":
		if out.State == "absent" {
			out.State = "reserved"
			out.Token = "stage-hold-" + in.Binding.Key
			p.reserves++
		}
		hook = p.afterReserve
		p.afterReserve = nil
		lost = p.loseACK
		p.loseACK = false
	case "commit":
		if out.State == "reserved" {
			out.State = "committed"
		}
	case "release":
		if out.State != "committed" {
			out.State = "released"
		}
	}
	p.states[in.Binding.Key] = out
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	if lost {
		return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
	}
	return out, nil
}
func newAutomaticFiniteFixture(t *testing.T, base orderFixture, balance int64) (automaticRecurringFixture, *stagedFiniteProvider) {
	t.Helper()
	f := newAutomaticRecurringFixture(t, base, balance)
	p := &stagedFiniteProvider{base: f.quotes, db: f.db, states: map[string]commerce.DiscountRedemptionResult{}, finite: true, probe: true}
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	f.auto = commerce.AutomaticRenewals{Repository: commercestore.AutomaticRenewals{DB: f.db, Issuer: issuer, Now: func() time.Time { return *f.at }, Discounts: p}}
	f.orders = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: func() time.Time { return *f.at }, Discounts: p}}
	return f, p
}
func automaticStageRecord(t *testing.T, f automaticRecurringFixture) model.SubscriptionAutomaticRenewal {
	t.Helper()
	var row model.SubscriptionAutomaticRenewal
	if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
func automaticFiniteBalance(t *testing.T, f automaticRecurringFixture, want int64) {
	t.Helper()
	var wallet model.WalletAccount
	if err := f.db.First(&wallet, "user_id = ? AND currency = ?", 1, "CNY").Error; err != nil || wallet.BalanceMinor != want || wallet.ReservedMinor != 0 {
		t.Fatalf("staged financial invariant: %+v %v want=%d", wallet, err, want)
	}
}
func stageFiniteOrder(t *testing.T, f automaticRecurringFixture) model.SubscriptionAutomaticRenewal {
	t.Helper()
	f.enable(t)
	*f.at = f.sub.EndAt
	if n, err := f.auto.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("stage preparation: %d %v", n, err)
	}
	row := automaticStageRecord(t, f)
	if row.PendingOrderID == nil || row.PendingConsentRevision != row.Revision || row.PendingEndAt == nil || !row.PendingEndAt.Equal(f.sub.EndAt) || row.LastOrderID != nil || row.State != "discount_reserved" {
		t.Fatalf("queued state is not success: %+v", row)
	}
	var funds int64
	if err := f.db.Model(&model.OrderFunding{}).Where("order_id = ?", *row.PendingOrderID).Count(&funds).Error; err != nil || funds != 0 {
		t.Fatal("staging reserved wallet", funds, err)
	}
	return row
}

func TestAutomaticFiniteSQLiteStageRestartConfirmedOfflineAndIdempotency(t *testing.T) {
	assertAutomaticFiniteRestart(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteStageRestartConfirmedOfflineAndIdempotency(t *testing.T) {
	assertAutomaticFiniteRestart(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteRestart(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	row := stageFiniteOrder(t, f)
	automaticFiniteBalance(t, f, 40000)
	calls := f.quotes.calls.Load()
	p.offline = true
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	restarted := commerce.AutomaticRenewals{Repository: commercestore.AutomaticRenewals{DB: f.db, Issuer: issuer, Now: func() time.Time { return *f.at }, Discounts: p}}
	*f.at = f.at.Add(2 * time.Minute)
	status, err := restarted.Read(t.Context(), 1, f.sub.ID, base.skuRecord.ID)
	if err != nil || status.PendingOrderID == nil || status.State != "discount_reserved" || status.Offer != nil || f.quotes.calls.Load() != calls {
		t.Fatalf("pending read incorrectly requoted: %+v %v", status, err)
	}
	if n, err := restarted.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("confirmed offline restart: %d %v", n, err)
	}
	successful := automaticStageRecord(t, f)
	if successful.PendingOrderID != nil || successful.LastOrderID == nil || *successful.LastOrderID != *row.PendingOrderID || successful.State != "renewed" || successful.PendingAttempt != 1 {
		t.Fatalf("successful staged renewal: %+v", successful)
	}
	automaticFiniteBalance(t, f, 30000)
	if n, err := restarted.Process(t.Context(), 10); err != nil || n != 0 {
		t.Fatalf("duplicate staged debit: %d %v", n, err)
	}
	if f.quotes.calls.Load() != calls || p.reserves != 1 {
		t.Fatal("accepted hold was requoted/reserved again")
	}
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", *row.PendingOrderID).Error; err != nil || intent.Desired != "commit" {
		t.Fatalf("payment lost durable commit: %+v %v", intent, err)
	}
}

func TestAutomaticFiniteSQLiteRevokeDuringReserveWithoutHistoricalValuation(t *testing.T) {
	assertAutomaticFiniteRevoke(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteRevokeDuringReserveWithoutHistoricalValuation(t *testing.T) {
	assertAutomaticFiniteRevoke(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteRevoke(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	approval := f.enable(t)
	*f.at = f.sub.EndAt
	p.afterReserve = func() {
		if _, err := f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{ExpectedRevision: approval.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := f.auto.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("reserve/revoke: %d %v", n, err)
	}
	revoked := automaticStageRecord(t, f)
	if revoked.Enabled || revoked.PendingOrderID == nil || revoked.Revision == revoked.PendingConsentRevision {
		t.Fatalf("revocation did not persist ahead of cleanup: %+v", revoked)
	}
	// Synthetic unpaid attempts are not fabricated purchase/payment evidence.
	failed := make([]model.Order, 1200)
	for i := range failed {
		target := f.sub.ID
		failed[i] = model.Order{UserID: 1, PlanID: base.planRecord.ID, PlanSKUID: base.skuRecord.ID, TargetSubscriptionID: &target, OrderType: "renewal", TradeNo: fmt.Sprintf("stage-unpaid-history-%d", i), Status: "failed", Currency: "CNY", PurchaseSnapshot: "untrusted unpaid historical attempt", CreatedAt: *f.at}
	}
	if err := f.db.CreateInBatches(&failed, 100).Error; err != nil {
		t.Fatal(err)
	}
	// Even this pending order's oversized nonfinancial material is retained; its
	// cancel-only path uses order header+65 binding, never all source snapshots.
	huge := strings.Repeat("retained-unpaid-material", commerce.MaxPurchaseSnapshotBytes/4)
	if err := f.db.Model(&model.Order{}).Where("id = ?", *revoked.PendingOrderID).Update("purchase_snapshot", huge).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := f.auto.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("disabled cleanup depended on historical budget: %d %v", n, err)
	}
	closed := automaticStageRecord(t, f)
	if closed.PendingOrderID != nil || closed.Enabled || closed.LastOrderID != nil {
		t.Fatalf("revoked stage survived: %+v", closed)
	}
	var order model.Order
	if err := f.db.Select("id,status,purchase_snapshot").First(&order, *revoked.PendingOrderID).Error; err != nil || order.Status != "canceled" || order.PurchaseSnapshot != huge {
		t.Fatalf("cleanup changed evidence: %+v %v", order, err)
	}
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", order.ID).Error; err != nil || intent.Desired != "release" {
		t.Fatalf("revocation lost release: %+v %v", intent, err)
	}
	automaticFiniteBalance(t, f, 40000)
}

func TestAutomaticFiniteSQLiteManualRenewalInvalidatesOldStage(t *testing.T) {
	assertAutomaticFiniteManual(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteManualRenewalInvalidatesOldStage(t *testing.T) {
	assertAutomaticFiniteManual(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteManual(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	stage := stageFiniteOrder(t, f)
	renewed := f.buy(t, f.sub.ID, "manual-beats-staged-auto")
	*f.at = f.at.Add(2 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("old stage after manual renewal: %d %v", n, err)
	}
	row := automaticStageRecord(t, f)
	if row.PendingOrderID != nil || row.LastOrderID != nil || !row.Enabled {
		t.Fatalf("manual payment mislabeled as auto or old stage retained: %+v", row)
	}
	var old model.Order
	if err := f.db.Select("id,status").First(&old, *stage.PendingOrderID).Error; err != nil || old.Status != "canceled" {
		t.Fatal(old, err)
	}
	var sub model.Subscription
	if err := f.db.First(&sub, f.sub.ID).Error; err != nil || !row.NextAttemptAt.Equal(sub.EndAt) || renewed.SubscriptionID != sub.ID {
		t.Fatal("future renewal schedule lost", err)
	}
	automaticFiniteBalance(t, f, 30000)
	if p.reserves != 2 {
		t.Fatal("manual and queued orders shared a reservation", p.reserves)
	}
}

func TestAutomaticFiniteSQLiteUnknownACKAndExpiredStageRetry(t *testing.T) {
	assertAutomaticFiniteUnknownAndRetry(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteUnknownACKAndExpiredStageRetry(t *testing.T) {
	assertAutomaticFiniteUnknownAndRetry(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteUnknownAndRetry(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	f.enable(t)
	*f.at = f.sub.EndAt
	p.loseACK = true
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unknown ACK not retained: %d %v", n, err)
	}
	first := automaticStageRecord(t, f)
	if first.PendingOrderID == nil || first.LastOrderID != nil {
		t.Fatalf("unknown stage: %+v", first)
	}
	automaticFiniteBalance(t, f, 40000)
	*f.at = f.at.Add(2 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("same-key query confirmation: %d %v", n, err)
	}
	confirmed := automaticStageRecord(t, f)
	if confirmed.PendingOrderID == nil || *confirmed.PendingOrderID != *first.PendingOrderID || confirmed.State != "discount_reserved" || p.reserves != 1 {
		t.Fatalf("unknown created duplicate order/hold: %+v %d", confirmed, p.reserves)
	}
	// No funding was prepared; the authoritative30m gate expires this attempt.
	*f.at = f.at.Add(31 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("expired stage cleanup: %d %v", n, err)
	}
	closed := automaticStageRecord(t, f)
	if closed.PendingOrderID != nil || !closed.Enabled || closed.PendingAttempt != 1 || closed.LastOrderID != nil {
		t.Fatalf("expired attempt permanently stuck or marked successful: %+v", closed)
	}
	jobs := commercestore.DiscountRedemptionJobs{DB: f.db, Provider: p, Now: func() time.Time { return *f.at }}
	if err := jobs.Step(t.Context(), *first.PendingOrderID); err != nil {
		t.Fatal(err)
	}
	*f.at = f.at.Add(2 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("new traceable attempt within window: %d %v", n, err)
	}
	retried := automaticStageRecord(t, f)
	if retried.PendingOrderID == nil || *retried.PendingOrderID == *first.PendingOrderID || retried.PendingAttempt != 2 || p.reserves != 2 {
		t.Fatalf("old idempotency key prevented retry: %+v", retried)
	}
	*f.at = f.at.Add(2 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("retry fulfillment: %d %v", n, err)
	}
	successful := automaticStageRecord(t, f)
	if successful.LastOrderID == nil || *successful.LastOrderID != *retried.PendingOrderID || successful.PendingOrderID != nil {
		t.Fatalf("retry success identity: %+v", successful)
	}
	automaticFiniteBalance(t, f, 30000)
}

func TestAutomaticFiniteSQLiteSlowProviderMovesQueueAndStableBalanceCompetition(t *testing.T) {
	assertAutomaticFiniteQueue(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteSlowProviderMovesQueueAndStableBalanceCompetition(t *testing.T) {
	assertAutomaticFiniteQueue(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteQueue(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 30000)
	p.finite = false
	secondRoot := f.buy(t, 0, "second-stable-auto-root")
	p.finite = true
	second := f
	second.sub = model.Subscription{}
	second.root = secondRoot
	if err := f.db.First(&second.sub, secondRoot.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	f.enable(t)
	second.enable(t)
	*f.at = f.sub.EndAt
	p.afterReserve = func() { *f.at = f.at.Add(2 * time.Minute) }
	if n, err := f.auto.Process(t.Context(), 2); n != 2 || err != nil {
		t.Fatalf("slow provider starved neighbor: %d %v", n, err)
	}
	one, two := automaticStageRecord(t, f), automaticStageRecord(t, second)
	if one.PendingOrderID == nil || two.PendingOrderID == nil || !one.NextAttemptAt.After(*f.at) || !two.NextAttemptAt.After(*f.at) {
		t.Fatalf("post-RPC backoff was based on old clock: first=%+v second=%+v now=%s", one, two, *f.at)
	}
	automaticFiniteBalance(t, f, 10000)
	*f.at = f.at.Add(2 * time.Minute)
	if n, err := f.auto.Process(t.Context(), 2); n != 2 || err != nil {
		t.Fatalf("stable balance competition: %d %v", n, err)
	}
	one, two = automaticStageRecord(t, f), automaticStageRecord(t, second)
	if one.LastOrderID == nil || two.LastOrderID != nil || two.State != "insufficient_balance" || two.PendingOrderID == nil || one.AttemptSequence >= two.AttemptSequence {
		t.Fatalf("wallet winner was not stable subscription order: first=%+v second=%+v", one, two)
	}
	automaticFiniteBalance(t, f, 0)
	view, err := second.auto.Read(t.Context(), 1, second.sub.ID, base.skuRecord.ID)
	if err != nil || view.State != "insufficient_balance" || view.PendingOrderID == nil {
		t.Fatalf("confirmed status hid insufficient balance: %+v %v", view, err)
	}
}

func TestAutomaticFiniteSQLiteConcurrentExecutorsPayOnce(t *testing.T) {
	assertAutomaticFiniteConcurrent(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteConcurrentExecutorsPayOnce(t *testing.T) {
	assertAutomaticFiniteConcurrent(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteConcurrent(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	f.enable(t)
	*f.at = f.sub.EndAt
	p.probe = false
	run := func() {
		var wg sync.WaitGroup
		errs := make(chan error, 6)
		for range 6 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := f.auto.Process(t.Context(), 2); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	run()
	queued := automaticStageRecord(t, f)
	if queued.PendingOrderID == nil || queued.PendingAttempt != 1 || p.reserves != 1 {
		t.Fatalf("concurrent creation duplicated stage: %+v holds=%d", queued, p.reserves)
	}
	automaticFiniteBalance(t, f, 40000)
	*f.at = f.at.Add(2 * time.Minute)
	run()
	settled := automaticStageRecord(t, f)
	if settled.PendingOrderID != nil || settled.LastOrderID == nil || *settled.LastOrderID != *queued.PendingOrderID || settled.PendingAttempt != 1 {
		t.Fatalf("concurrent duplicate fulfillment: %+v", settled)
	}
	automaticFiniteBalance(t, f, 30000)
	var paid int64
	if err := f.db.Model(&model.Order{}).Where("target_subscription_id = ? AND order_type = ? AND status = ?", f.sub.ID, "renewal", "paid").Count(&paid).Error; err != nil || paid != 1 {
		t.Fatalf("repeated paid renewal: %d %v", paid, err)
	}
}

func TestAutomaticFiniteSQLiteClosedWindowAndUnsubscribePreventOldStage(t *testing.T) {
	for _, mode := range []string{"window", "unsubscribe", "replacement"} {
		t.Run(mode, func(t *testing.T) { assertAutomaticFiniteTerminal(t, newOrderFixture(t), mode) })
	}
}
func TestMySQLAutomaticFiniteClosedWindowAndUnsubscribePreventOldStage(t *testing.T) {
	for _, mode := range []string{"window", "unsubscribe", "replacement"} {
		t.Run(mode, func(t *testing.T) { assertAutomaticFiniteTerminal(t, newMySQLBusinessOrderFixture(t), mode) })
	}
}
func assertAutomaticFiniteTerminal(t *testing.T, base orderFixture, mode string) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	stage := stageFiniteOrder(t, f)
	expected := int64(40000)
	expectedReserves := 1
	switch mode {
	case "window":
		*f.at = f.at.Add(8 * 24 * time.Hour)
	case "unsubscribe":
		values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: func() time.Time { return *f.at }}}
		q, err := values.Read(t.Context(), 1, f.sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		terminate := commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: func() time.Time { return *f.at }}}
		if _, err := terminate.Terminate(t.Context(), 1, f.sub.ID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "unsubscribe-pending-auto"}); err != nil {
			t.Fatal(err)
		}
	case "replacement":
		// A due staged renewal is expired, and existing product rules correctly
		// reject replacing expired rights. Restore it through a real manual renewal
		// before changing plans; retain the stale stage to verify its cancellation.
		f.buy(t, f.sub.ID, "restore-before-staged-replacement")
		expectedReserves++
		plan := base.plan(t, 2)
		sku := base.sku(t, plan.ID, 2000, "change")
		request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: f.sub.ID, IdempotencyKey: "replacement-pending-auto"}
		q, err := f.orders.Preview(t.Context(), 1, request)
		if err != nil {
			t.Fatal(err)
		}
		if q.SettlementAmount != 10000 || q.CreditAmount != 2000 {
			t.Fatalf("replacement must use real remaining paid value: %+v", q)
		}
		request.QuoteFingerprint = q.QuoteFingerprint
		order, err := f.orders.Create(t.Context(), 1, request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
		expected -= 2000
	}
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("terminal stage cleanup: %s %d %v", mode, n, err)
	}
	closed := automaticStageRecord(t, f)
	if closed.Enabled || closed.PendingOrderID != nil || closed.LastOrderID != nil {
		t.Fatalf("terminal action retained automatic debit: %+v", closed)
	}
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", *stage.PendingOrderID).Error; err != nil || intent.Desired != "release" {
		t.Fatalf("terminal action lost release: %+v %v", intent, err)
	}
	automaticFiniteBalance(t, f, expected)
	if n, err := f.auto.Process(t.Context(), 10); n != 0 || err != nil {
		t.Fatalf("closed automatic subscription revived: %d %v", n, err)
	}
	if p.reserves != expectedReserves {
		t.Fatal("terminal stage reserved again", p.reserves)
	}
}

func TestAutomaticFiniteSQLiteHistoricalConsumedCouponAllowsLaterPureRenewal(t *testing.T) {
	assertAutomaticFiniteThenPure(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteHistoricalConsumedCouponAllowsLaterPureRenewal(t *testing.T) {
	assertAutomaticFiniteThenPure(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteThenPure(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 60000)
	finiteRoot := f.buy(t, 0, "finite-root-for-pure-auto")
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", finiteRoot.ID).Error; err != nil || intent.Desired != "commit" {
		t.Fatalf("finite root is not consumed: %+v %v", intent, err)
	}
	if err := (commercestore.DiscountRedemptionJobs{DB: f.db, Provider: p, Now: func() time.Time { return *f.at }}).Step(t.Context(), finiteRoot.ID); err != nil {
		t.Fatal(err)
	}
	f.sub = model.Subscription{}
	if err := f.db.First(&f.sub, finiteRoot.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	f.root = finiteRoot
	p.finite = false
	f.enable(t)
	*f.at = f.sub.EndAt
	holds := p.reserves
	if n, err := f.auto.Process(t.Context(), 1); err != nil || n != 1 {
		t.Fatalf("historical consumed coupon blocked a new pure renewal: %d %v", n, err)
	}
	row := automaticStageRecord(t, f)
	if row.PendingOrderID != nil || row.LastOrderID == nil || row.State != "renewed" || p.reserves != holds {
		t.Fatalf("pure renewal acquired an unnecessary hold: %+v", row)
	}
	automaticFiniteBalance(t, f, 30000)
}

func TestAutomaticFiniteSQLitePublicationFailureKeepsPendingAndRollsBackCommit(t *testing.T) {
	assertAutomaticFinitePublicationRollback(t, newOrderFixture(t))
}
func TestMySQLAutomaticFinitePublicationFailureKeepsPendingAndRollsBackCommit(t *testing.T) {
	assertAutomaticFinitePublicationRollback(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFinitePublicationRollback(t *testing.T, base orderFixture) {
	f, _ := newAutomaticFiniteFixture(t, base, 50000)
	stage := stageFiniteOrder(t, f)
	*f.at = f.at.Add(2 * time.Minute)
	failure := errors.New("staged publication persistence failure")
	const callback = "staged-auto-publication-failure"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "node_config_publishes" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callback) })
	if _, err := f.auto.Process(t.Context(), 1); !errors.Is(err, failure) {
		t.Fatalf("publication failure was not propagated: %v", err)
	}
	kept := automaticStageRecord(t, f)
	if kept.PendingOrderID == nil || *kept.PendingOrderID != *stage.PendingOrderID || kept.LastOrderID != nil {
		t.Fatalf("failed payment dropped pending recovery: %+v", kept)
	}
	var intent model.OrderDiscountRedemption
	if err := f.db.First(&intent, "order_id = ?", *stage.PendingOrderID).Error; err != nil || intent.Desired != "reserve" || intent.Observed != "reserved" {
		t.Fatalf("commit intent escaped rollback: %+v %v", intent, err)
	}
	automaticFiniteBalance(t, f, 40000)
	if err := f.db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		t.Fatalf("same pending recovery after DB failure: %d %v", n, err)
	}
	automaticFiniteBalance(t, f, 30000)
}

type stagedHeadAdvanceContextKey struct{}

func TestAutomaticFiniteSQLiteQueueAdvanceIsNotDamagedAuthority(t *testing.T) {
	assertAutomaticFiniteHeadAdvance(t, newOrderFixture(t))
}
func TestMySQLAutomaticFiniteQueueAdvanceIsNotDamagedAuthority(t *testing.T) {
	assertAutomaticFiniteHeadAdvance(t, newMySQLBusinessOrderFixture(t))
}
func assertAutomaticFiniteHeadAdvance(t *testing.T, base orderFixture) {
	f, p := newAutomaticFiniteFixture(t, base, 50000)
	f.enable(t)
	*f.at = f.sub.EndAt
	p.loseACK = true
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unknown setup: %d %v", n, err)
	}
	*f.at = f.at.Add(2 * time.Minute)
	p.probe = false
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	pause := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(stagedHeadAdvanceContextKey{}) != true {
			return
		}
		once.Do(func() {
			close(entered)
			select {
			case <-resume:
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
			}
		})
	}
	// SQLite begins IMMEDIATE, so pause the unlocked scope metadata read
	// before entering a transaction; do not wait after BEGIN owns its writer.
	const queryHook = "auto-stage-after-unlocked-scope-metadata"
	if err := f.db.Callback().Query().After("gorm:query").Register(queryHook, func(tx *gorm.DB) {
		if tx.Statement.Table == "orders" && strings.Contains(tx.Statement.SQL.String(), "AS evidence_bytes") && tx.RowsAffected > 0 {
			pause(tx)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(queryHook) })
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), stagedHeadAdvanceContextKey{}, true), 20*time.Second)
	defer cancel()
	type outcome struct {
		n   int
		err error
	}
	done := make(chan outcome, 1)
	go func() { n, err := f.auto.Process(ctx, 1); done <- outcome{n, err} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		cancel()
		close(resume)
		t.Fatal("first worker did not reach pre-guard barrier")
	}
	// The neighbor confirms the existing hold and moves the same queue head's
	// NextAttemptAt forward, without changing sources, orders, term or epoch.
	if n, err := f.auto.Process(t.Context(), 1); n != 1 || err != nil {
		cancel()
		close(resume)
		t.Fatalf("neighbor advance: %d %v", n, err)
	}
	advanced := automaticStageRecord(t, f)
	if advanced.State != "discount_reserved" {
		cancel()
		close(resume)
		t.Fatalf("neighbor failed to confirm: %+v", advanced)
	}
	close(resume)
	result := <-done
	if result.err != nil || result.n != 0 {
		t.Fatalf("normal empty head was not bounded reselect: %+v", result)
	}
	final := automaticStageRecord(t, f)
	if final.State != advanced.State || !final.NextAttemptAt.Equal(advanced.NextAttemptAt) || final.PendingOrderID == nil || *final.PendingOrderID != *advanced.PendingOrderID {
		t.Fatalf("queue advance mislabeled damaged authority: before=%+v after=%+v", advanced, final)
	}
	automaticFiniteBalance(t, f, 40000)
}
