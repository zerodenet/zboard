package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// In-memory protocol fixture; real provider persistence/restart durability requires external acceptance.
// Financial orders, wallet ledger, receipts, quotes and fulfillment are real core
// paths. Native transport/admission has separate SDK and signed manifest tests.
type exclusiveDiscountFixture struct {
	states         map[string]commerce.DiscountRedemptionResult
	calls          []string
	loseReserveACK bool
	offline        bool
}

func (p *exclusiveDiscountFixture) EvaluateCommerceDiscount(_ context.Context, _ string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	terms := `{"campaign":"provider-owned-finite","allocation":"exclusive_until_core_finalization_v1"}`
	hash, _ := commerce.DiscountTermsHash(terms)
	scope := "recurring"
	if in.Operation != "new" && in.Operation != "renewal" {
		scope = "order_only"
	}
	return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "example.exclusive", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Redemption: commerce.DiscountRedemptionGuarantee, Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: min(2000, in.BasePriceMinor), ExpiresAt: time.Now().UTC().Add(time.Hour), Agreement: &commerce.DiscountAgreement{ID: "finite-agreement", Version: "1", Scope: scope, TermsJSON: terms, TermsHash: hash}}}, nil
}
func (p *exclusiveDiscountFixture) RedeemDiscount(_ context.Context, r commerce.DiscountRedemptionRequest) (commerce.DiscountRedemptionResult, error) {
	p.calls = append(p.calls, r.Action+":"+r.Binding.Key)
	if p.offline {
		return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
	}
	if commerce.ValidateDiscountRedemptionRequest(r) != nil {
		return commerce.DiscountRedemptionResult{}, commerce.ErrDiscountRedemptionConflict
	}
	out, exists := p.states[r.Binding.Key]
	if !exists {
		out = commerce.DiscountRedemptionResult{Version: 1, Key: r.Binding.Key, BindingHash: r.BindingHash, State: "absent", Guarantee: commerce.DiscountRedemptionGuarantee}
	}
	if out.BindingHash != r.BindingHash {
		return out, commerce.ErrDiscountRedemptionConflict
	}
	switch r.Action {
	case "reserve":
		if out.State == "absent" {
			out.State = "reserved"
			out.Token = "opaque-exclusive-" + r.Binding.Key
		}
	case "commit":
		if out.State == "reserved" {
			out.State = "committed"
		}
	case "release":
		if out.State != "committed" {
			out.State = "released"
		}
	}
	p.states[r.Binding.Key] = out
	if r.Action == "reserve" && p.loseReserveACK {
		p.loseReserveACK = false
		return commerce.DiscountRedemptionResult{}, context.DeadlineExceeded
	}
	return out, nil
}

func TestDiscountRedemptionManualWalletReceiptLifecycle(t *testing.T) {
	assertDiscountRedemptionManualLifecycle(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionManualWalletReceiptLifecycle(t *testing.T) {
	assertDiscountRedemptionManualLifecycle(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountRedemptionManualLifecycle(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 12000)
	fundTestWallet(t, f, 40000)
	if err := f.h.db.Model(&f.planRecord).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}, loseReserveACK: true}
	f.h.services.ConfigureCommerceDiscountProvider(p)
	create := func(key string, target uint) commerce.Order {
		request := commerce.OrderCreateRequest{IdempotencyKey: key, PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: target}
		if target == 0 {
			request.Discount = &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}
		}
		preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
		if err != nil {
			t.Fatal(err)
		}
		request.QuoteFingerprint = preview.QuoteFingerprint
		out, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
		if err != nil || out.PayableAmount != 10000 {
			t.Fatalf("finite order: %+v %v", out, err)
		}
		return out
	}
	first := create("finite-initial-order", 0)
	if len(p.states) != 1 {
		t.Fatalf("preview must not reserve stock: %d", len(p.states))
	}
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, first.ID, commerce.FundingRequest{UseBalance: true}); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("unconfirmed funding gate: %v", err)
	}
	// Public owned endpoint returns409 and never reveals the provider hold token/key.
	response := httptest.NewRecorder()
	f.h.OrderFundingHandler(response, announcementRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%d/funding", first.ID), f.token, `{"use_balance":true}`))
	if response.Code != 409 || !strings.Contains(response.Body.String(), "discount_reservation_pending") || strings.Contains(response.Body.String(), "opaque-exclusive") {
		t.Fatalf("pending API: %d %s", response.Code, response.Body.String())
	}
	projection, err := wallet.Read(t.Context(), 1, first.ID)
	if err != nil || projection.DiscountReservation == nil || projection.DiscountReservation.CanPay || !projection.DiscountReservation.CanCancel || projection.DiscountReservation.CanReconcile {
		t.Fatalf("pending projection: %+v %v", projection, err)
	}
	public, _ := json.Marshal(projection)
	if strings.Contains(string(public), "binding") || strings.Contains(string(public), "example.exclusive") || strings.Contains(string(public), "opaque-exclusive") {
		t.Fatal("public intent internals leaked", string(public))
	}
	at := time.Now().UTC().Add(20 * time.Second)
	jobs := commercestore.DiscountRedemptionJobs{DB: f.h.db, Provider: application.CommerceDiscountProvider{Native: p}, Now: func() time.Time { return at }}
	if err := jobs.Step(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	reserves := 0
	for _, call := range p.calls {
		if strings.HasPrefix(call, "reserve:") {
			reserves++
		}
	}
	if reserves != 1 {
		t.Fatalf("lost ACK retried Reserve instead of Query: %v", p.calls)
	}
	// Once confirmed the saved price fulfills while the provider is offline.
	p.offline = true
	if _, err := wallet.Configure(t.Context(), 1, first.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	paid, err := wallet.Pay(t.Context(), 1, first.ID)
	if err != nil || !paid.Fulfilled {
		t.Fatalf("locked confirmed price: %+v %v", paid, err)
	}
	if duplicate, err := wallet.Pay(t.Context(), 1, first.ID); err != nil || duplicate.Fulfilled {
		t.Fatalf("wallet replay: %+v %v", duplicate, err)
	}

	var beforeCommit model.OrderDiscountRedemption
	if err := f.h.db.First(&beforeCommit, "order_id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if beforeCommit.Desired != "commit" || beforeCommit.Observed != "reserved" {
		t.Fatalf("payment did not atomically queue commit: desired=%s observed=%s", beforeCommit.Desired, beforeCommit.Observed)
	}
	// Advance the trusted executor clock to its actual due/lease boundary; a
	// scheduler no-op cannot prove an offline mutation was attempted.
	if at.Before(beforeCommit.NextAttemptAt) {
		at = beforeCommit.NextAttemptAt
	}
	if at.Before(beforeCommit.LeaseUntil) {
		at = beforeCommit.LeaseUntil
	}
	at = at.Add(time.Millisecond)
	if err := jobs.Step(t.Context(), first.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("offline commit intent: %v", err)
	}
	p.offline = false
	at = at.Add(20 * time.Second)
	if err := jobs.Step(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	var intent model.OrderDiscountRedemption
	if err := f.h.db.First(&intent, "order_id = ?", first.ID).Error; err != nil || intent.Desired != "commit" || intent.Observed != "committed" {
		t.Fatalf("commit recovery: %+v %v", intent, err)
	}
	// Manual inherited recurring discount obtains a separate order-bound hold.
	renewal := create("finite-manual-renewal", paid.SubscriptionID)
	if _, err := wallet.Configure(t.Context(), 1, renewal.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if result, err := wallet.Pay(t.Context(), 1, renewal.ID); err != nil || !result.Fulfilled || result.SubscriptionID != paid.SubscriptionID {
		t.Fatalf("manual finite renewal: %+v %v", result, err)
	}
	// A failed admitted cash attempt retains reconciliation without reopening
	// collection. Late verified cash credits once and finalizes Release.
	late := create("finite-canceled-cash", 0)
	if _, err := wallet.Configure(t.Context(), 1, late.ID, commerce.FundingRequest{UseBalance: false}); err != nil {
		t.Fatal(err)
	}
	prepareReceiptAttempt(t, f, late.ID, "finite-cash")
	failedFact := commerce.PaymentFact{OrderID: late.ID, ProviderEventID: "finite-failure-event", ProviderTradeNo: "finite-late-trade", Status: "failed", AmountMinor: 10000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	if _, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "finite-cash", RequireAttempt: true}, failedFact); err != nil {
		t.Fatal(err)
	}
	recoveryView, err := wallet.Read(t.Context(), 1, late.ID)
	if err != nil || recoveryView.DiscountReservation == nil || recoveryView.DiscountReservation.CanPay || recoveryView.DiscountReservation.CanCancel || !recoveryView.DiscountReservation.CanReconcile {
		t.Fatalf("failed existing-attempt recovery actions: %+v %v", recoveryView.DiscountReservation, err)
	}
	// Currency/amount mismatch cannot turn an unrelated/corrupt attempt into
	// public reconciliation authority. Restore only this owned fixture row.
	for _, mutation := range []map[string]any{{"currency": "USD"}, {"amount_minor": int64(9999)}} {
		if err := f.h.db.Model(&model.PaymentAttempt{}).Where("order_id = ? AND provider = ?", late.ID, "finite-cash").UpdateColumns(mutation).Error; err != nil {
			t.Fatal(err)
		}
		mismatch, err := wallet.Read(t.Context(), 1, late.ID)
		if err != nil || mismatch.DiscountReservation == nil || mismatch.DiscountReservation.CanReconcile {
			t.Fatalf("mismatched cash attempt offered reconciliation: %+v %v", mismatch.DiscountReservation, err)
		}
		if err := f.h.db.Model(&model.PaymentAttempt{}).Where("order_id = ? AND provider = ?", late.ID, "finite-cash").UpdateColumns(map[string]any{"currency": "CNY", "amount_minor": int64(10000)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	publicRecovery, _ := json.Marshal(recoveryView)
	for _, hidden := range []string{"finite-cash", "example.exclusive", "opaque-exclusive", "binding_hash", "redemption_key", "lease_token"} {
		if strings.Contains(string(publicRecovery), hidden) {
			t.Fatal("recovery view leaked internal authority", hidden)
		}
	}
	recovered, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, late.ID, "finite-cash")
	if err != nil || !permitted || recovered.Status != "canceled" || recovered.PayableAmount != 10000 {
		t.Fatalf("closed failed attempt lost original cash reconciliation: %+v %v %v", recovered, permitted, err)
	}
	if _, permitted, err := f.h.services.PaymentCheckout.Open(t.Context(), 1, late.ID, "another-provider"); permitted || !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("closed hold admitted a new collection: permitted=%v %v", permitted, err)
	}
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, late.ID); !errors.Is(err, commerce.ErrOrderNotCancelable) {
		t.Fatalf("failed-order cancellation permissions changed: %v", err)
	}
	at = at.Add(20 * time.Second)
	if err := jobs.Step(t.Context(), late.ID); err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: late.ID, ProviderEventID: "finite-late-event", ProviderTradeNo: "finite-late-trade", Status: "paid", AmountMinor: 10000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	for range 2 {
		result, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "finite-cash", RequireAttempt: true}, fact)
		if err != nil || result.Fulfilled || result.Disposition != "wallet_credit" {
			t.Fatalf("late verified cash: %+v %v", result, err)
		}
	}
	var closed model.Order
	if err := f.h.db.First(&closed, late.ID).Error; err != nil || closed.Status != "canceled" || closed.SubscriptionID != 0 {
		t.Fatalf("resurrected canceled finite order: %+v %v", closed, err)
	}
	balance, err := f.h.services.Wallet.Read(t.Context(), 1, "CNY", 0, 100)
	if err != nil || balance.BalanceMinor != 30000 || balance.ReservedMinor != 0 {
		t.Fatalf("cash/wallet conservation: %+v %v", balance, err)
	}
	at = at.Add(time.Minute)
	if _, err := jobs.RunBatch(t.Context(), 10); err != nil {
		t.Fatal("remaining recovery", err)
	}
	var renewalIntent model.OrderDiscountRedemption
	if err := f.h.db.First(&renewalIntent, "order_id = ?", renewal.ID).Error; err != nil || renewalIntent.Desired != "commit" || renewalIntent.Observed != "committed" {
		t.Fatalf("renewal finalization not recovered: desired=%s observed=%s err=%v", renewalIntent.Desired, renewalIntent.Observed, err)
	}
}

func TestDiscountRedemptionZeroAmountAndUnfundedCancellation(t *testing.T) {
	assertDiscountRedemptionZeroAndCancel(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionZeroAmountAndUnfundedCancellation(t *testing.T) {
	assertDiscountRedemptionZeroAndCancel(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountRedemptionZeroAndCancel(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 2000)
	if err := f.h.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}, loseReserveACK: true}
	f.h.services.ConfigureCommerceDiscountProvider(p)
	create := func(key string) commerce.Order {
		request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: key, Discount: &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}}
		preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
		if err != nil {
			t.Fatal(err)
		}
		request.QuoteFingerprint = preview.QuoteFingerprint
		order, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
		if err != nil || order.PayableAmount != 0 {
			t.Fatalf("free discounted quote: %+v %v", order, err)
		}
		return order
	}
	order := create("finite-zero-confirmation")
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{}); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("zero amount skipped reservation gate: %v", err)
	}
	if _, err := f.h.services.OrderSettlement(f.h.credentialCipher, f.h.zeroMieruAccess).Apply(t.Context(), 1, commerce.SettlementCommand{OrderID: order.ID, Status: "paid"}); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("administrator zero amount bypass: %v", err)
	}
	at := time.Now().UTC().Add(20 * time.Second)
	jobs := commercestore.DiscountRedemptionJobs{DB: f.h.db, Provider: application.CommerceDiscountProvider{Native: p}, Now: func() time.Time { return at }}
	if err := jobs.Step(t.Context(), order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := wallet.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{}); err != nil {
		t.Fatal(err)
	}
	if result, err := wallet.Pay(t.Context(), 1, order.ID); err != nil || !result.Fulfilled {
		t.Fatalf("confirmed zero amount: %+v %v", result, err)
	}
	p.offline = true
	canceled := create("finite-unfunded-cancel")
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, canceled.ID); err != nil {
		t.Fatal(err)
	}
	var intent model.OrderDiscountRedemption
	if err := f.h.db.First(&intent, "order_id = ?", canceled.ID).Error; err != nil || intent.Desired != "release" || intent.Token != "" {
		t.Fatalf("cancel before Reserve/without funding lost release intent: %+v %v", intent, err)
	}
	p.offline = false
	at = at.Add(20 * time.Second)
	if err := jobs.Step(t.Context(), canceled.ID); err != nil {
		t.Fatal(err)
	}
	if p.states[intent.RedemptionKey].State != "released" {
		t.Fatal("unfunded cancellation failed to establish tombstone")
	}
	var binding commerce.DiscountRedemptionBinding
	if err := json.Unmarshal([]byte(intent.BindingJSON), &binding); err != nil {
		t.Fatal(err)
	}
	late, err := p.RedeemDiscount(t.Context(), commerce.DiscountRedemptionRequest{Version: 1, Action: "reserve", Binding: binding, BindingHash: intent.BindingHash})
	if err != nil || late.State != "released" {
		t.Fatalf("delayed Reserve reused canceled quota: %+v %v", late, err)
	}
}

func TestDiscountRedemptionPublishFailureRollsBackMoneyAndCommitIntent(t *testing.T) {
	assertDiscountRedemptionPublishRollback(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionPublishFailureRollsBackMoneyAndCommitIntent(t *testing.T) {
	assertDiscountRedemptionPublishRollback(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountRedemptionPublishRollback(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 12000)
	fundTestWallet(t, f, 20000)
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}}
	f.h.services.ConfigureCommerceDiscountProvider(p)
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "finite-publish-rollback", Discount: &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}}
	preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	order, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	wallet := fundingService(f)
	if _, err := wallet.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected durable publication insert failure")
	attempted := false
	const callback = "finite-discount-publication-failure"
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "node_config_publishes" {
			attempted = true
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.h.db.Callback().Create().Remove(callback) })
	if _, err := wallet.Pay(t.Context(), 1, order.ID); !errors.Is(err, failure) || !attempted {
		t.Fatalf("publication fault not reached: attempted=%v %v", attempted, err)
	}
	var intent model.OrderDiscountRedemption
	var stored model.Order
	var account model.WalletAccount
	if err := f.h.db.First(&intent, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&stored, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&account, "user_id = ? AND currency = ?", 1, "CNY").Error; err != nil {
		t.Fatal(err)
	}
	if intent.Desired != "reserve" || intent.Observed != "reserved" || stored.Status != "pending" || stored.SubscriptionID != 0 || account.BalanceMinor != 20000 || account.ReservedMinor != 10000 {
		t.Fatalf("partial financial commit: intent=%+v order=%+v account=%+v", intent, stored, account)
	}
	if err := f.h.db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if result, err := wallet.Pay(t.Context(), 1, order.ID); err != nil || !result.Fulfilled {
		t.Fatalf("retry original confirmed order: %+v %v", result, err)
	}
}

func TestDiscountRedemptionFullReplacementCannotSpendResidualBeforeConfirmation(t *testing.T) {
	assertDiscountRedemptionReplacementGate(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionFullReplacementCannotSpendResidualBeforeConfirmation(t *testing.T) {
	assertDiscountRedemptionReplacementGate(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountRedemptionReplacementGate(t *testing.T, base orderFixture) {
	f := newFullReplacementFixture(t, base, false, 0, 3)
	if err := f.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}, loseReserveACK: true}
	f.orders = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.db, Now: func() time.Time { return *f.at }, Discounts: application.CommerceDiscountProvider{Native: p}}}
	request := commerce.OrderCreateRequest{PlanSKUID: f.target, TargetSubscriptionID: f.subscription, IdempotencyKey: "finite-full-value-replacement", Discount: &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}}
	preview, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil || preview.ChangeMode != "full_term" || preview.PayableAmount != 0 || preview.SettlementAmount != 9000 {
		t.Fatalf("fully discounted replacement: %+v %v", preview, err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	order, err := f.orders.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	var before model.WalletAccount
	if err := f.db.First(&before, "user_id = ? AND currency = ?", 1, "CNY").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("residual bypassed unconfirmed coupon: %v", err)
	}
	var unchanged model.Subscription
	if err := f.db.First(&unchanged, f.subscription).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.PlanID != base.planRecord.ID || unchanged.Status != "active" {
		t.Fatalf("unconfirmed coupon terminated old rights: %+v", unchanged)
	}
	jobs := commercestore.DiscountRedemptionJobs{DB: f.db, Provider: application.CommerceDiscountProvider{Native: p}, Now: func() time.Time { return f.at.Add(20 * time.Second) }}
	if err := jobs.Step(t.Context(), order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.payments.Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	var after model.WalletAccount
	if err := f.db.First(&after, "user_id = ? AND currency = ?", 1, "CNY").Error; err != nil || after.BalanceMinor-before.BalanceMinor != 9000 || after.ReservedMinor != 0 {
		t.Fatalf("coupon converted into balance or repeated old value: before=%+v after=%+v %v", before, after, err)
	}
}

func TestDiscountRedemptionLostHoldAndHardPreparationWindow(t *testing.T) {
	assertDiscountLostHoldAndWindow(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionLostHoldAndHardPreparationWindow(t *testing.T) {
	assertDiscountLostHoldAndWindow(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountLostHoldAndWindow(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 12000)
	fundTestWallet(t, f, 20000)
	if err := f.h.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}}
	f.h.services.ConfigureCommerceDiscountProvider(p)
	create := func(key string) commerce.Order {
		r := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: key, Discount: &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}}
		q, err := f.h.services.OrderCreation.Preview(t.Context(), 1, r)
		if err != nil {
			t.Fatal(err)
		}
		r.QuoteFingerprint = q.QuoteFingerprint
		o, err := f.h.services.OrderCreation.Create(t.Context(), 1, r)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	order := create("finite-provider-durable-loss")
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var intent model.OrderDiscountRedemption
	if err := f.h.db.First(&intent, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	delete(p.states, intent.RedemptionKey) // provider violates its signed durable guarantee
	at := time.Now().UTC().Add(2 * time.Minute)
	jobs := commercestore.DiscountRedemptionJobs{DB: f.h.db, Provider: application.CommerceDiscountProvider{Native: p}, Now: func() time.Time { return at }}
	if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
		t.Fatal("lost hold silently accepted", err)
	}
	projection, err := fundingService(f).Read(t.Context(), 1, order.ID)
	if err != nil || projection.DiscountReservation == nil || projection.DiscountReservation.CanPay || projection.DiscountReservation.State != "unavailable" {
		t.Fatalf("lost hold still advertised payable: %+v %v", projection, err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("lost hold debited: %v", err)
	}
	at = at.Add(20 * time.Second)
	if err := jobs.Step(t.Context(), order.ID); !errors.Is(err, commerce.ErrDiscountRedemptionConflict) {
		t.Fatal(err)
	}
	reserves := 0
	for _, call := range p.calls {
		if call == "reserve:"+intent.RedemptionKey {
			reserves++
		}
	}
	if reserves != 1 {
		t.Fatalf("lost provider state allocated stock again: %v", p.calls)
	}
	if _, err := f.h.services.OrderCancellation.Owned(t.Context(), 1, order.ID); err != nil {
		t.Fatal(err)
	}
	delayed := create("finite-delayed-first-funding")
	future := time.Now().UTC().Add(31 * time.Minute)
	later := commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: f.h.db, Now: func() time.Time { return future }}}
	if _, err := later.Configure(t.Context(), 1, delayed.ID, commerce.FundingRequest{UseBalance: true}); !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
		t.Fatalf("late first prepare extended expired hold: %v", err)
	}
	var count int64
	if err := f.h.db.Model(&model.OrderFunding{}).Where("order_id = ?", delayed.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expired hold reserved money: %d %v", count, err)
	}
	legacy := f.create(t, 0)
	if _, err := later.Configure(t.Context(), 1, legacy.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatalf("finite policy changed ordinary v1 funding: %v", err)
	}
}

func TestDiscountRedemptionDamagedAuthorityRetainsCashButDatabaseFailureDoesNotACK(t *testing.T) {
	assertDiscountDamagedAuthorityReceipts(t, newOrderFixture(t))
}
func TestMySQLDiscountRedemptionDamagedAuthorityRetainsCashButDatabaseFailureDoesNotACK(t *testing.T) {
	assertDiscountDamagedAuthorityReceipts(t, newMySQLBusinessOrderFixture(t))
}
func assertDiscountDamagedAuthorityReceipts(t *testing.T, f orderFixture) {
	t.Helper()
	priceForFunding(t, f, 12000)
	if err := f.h.db.AutoMigrate(&model.OrderDiscountRedemption{}); err != nil {
		t.Fatal(err)
	}
	p := &exclusiveDiscountFixture{states: map[string]commerce.DiscountRedemptionResult{}}
	f.h.services.ConfigureCommerceDiscountProvider(p)
	create := func(key, provider string) commerce.Order {
		r := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: key, Discount: &commerce.OrderDiscountSelection{ProviderID: "example.exclusive"}}
		q, err := f.h.services.OrderCreation.Preview(t.Context(), 1, r)
		if err != nil {
			t.Fatal(err)
		}
		r.QuoteFingerprint = q.QuoteFingerprint
		o, err := f.h.services.OrderCreation.Create(t.Context(), 1, r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fundingService(f).Configure(t.Context(), 1, o.ID, commerce.FundingRequest{}); err != nil {
			t.Fatal(err)
		}
		prepareReceiptAttempt(t, f, o.ID, provider)
		return o
	}
	damaged := create("finite-damaged-authority-cash", "damaged-cash")
	if err := f.h.db.Model(&model.OrderDiscountRedemption{}).Where("order_id = ?", damaged.ID).Update("binding_json", strings.Repeat("x", commerce.MaxDiscountRedemptionBytes+1)).Error; err != nil {
		t.Fatal(err)
	}
	fact := commerce.PaymentFact{OrderID: damaged.ID, ProviderEventID: "damaged-real-cash", ProviderTradeNo: "damaged-real-trade", Status: "paid", AmountMinor: 10000, Currency: "CNY", OccurredAt: time.Now().UTC()}
	result, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "damaged-cash", RequireAttempt: true}, fact)
	if err != nil || result.Fulfilled || result.Disposition != "review_required" {
		t.Fatalf("real cash discarded or guessed through corrupt authority: %+v %v", result, err)
	}
	var receipt model.PaymentReceipt
	if err := f.h.db.Where("order_id = ?", damaged.ID).First(&receipt).Error; err != nil || receipt.AmountMinor != 10000 || receipt.Disposition != "review_required" {
		t.Fatalf("cash evidence absent: %+v %v", receipt, err)
	}
	transient := create("finite-database-transient-cash", "transient-cash")
	const callback = "finite-authority-database-unavailable"
	fault := errors.New("injected database connection failure")
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "order_discount_redemptions" {
			tx.AddError(fault)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.h.db.Callback().Query().Remove(callback) })
	fact.OrderID = transient.ID
	fact.ProviderEventID = "transient-real-cash"
	fact.ProviderTradeNo = "transient-real-trade"
	if _, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "transient-cash", RequireAttempt: true}, fact); !errors.Is(err, fault) {
		t.Fatalf("database failure falsely ACKed as review: %v", err)
	}
	if err := f.h.db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := f.h.db.Model(&model.PaymentReceipt{}).Where("order_id = ?", transient.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed commit falsely recorded receipt: %d %v", count, err)
	}
	// On a reliable retry the same verified fact can be accepted and fulfilled.
	if result, err := receiptService(f).Record(t.Context(), commerce.PaymentFactAuthority{AccountID: 1, Provider: "transient-cash", RequireAttempt: true}, fact); err != nil || !result.Fulfilled {
		t.Fatalf("reliable same-fact retry: %+v %v", result, err)
	}
}
