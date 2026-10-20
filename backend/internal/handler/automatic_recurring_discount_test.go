package handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type automaticRecurringQuoter struct {
	calls    atomic.Int64
	evaluate func(context.Context, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error)
}

func (q *automaticRecurringQuoter) Quote(ctx context.Context, _ string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	q.calls.Add(1)
	return q.evaluate(ctx, in)
}

type automaticRecurringFixture struct {
	base     orderFixture
	db       *gorm.DB
	at       *time.Time
	quotes   *automaticRecurringQuoter
	orders   commerce.OrderCreation
	payments commerce.WalletPayments
	auto     commerce.AutomaticRenewals
	sub      model.Subscription
	root     model.Order
}

func newAutomaticRecurringFixture(t *testing.T, base orderFixture, balance int64) automaticRecurringFixture {
	t.Helper()
	priceForFunding(t, base, 12000)
	if err := base.h.db.Model(&base.planRecord).Update("is_renewable", true).Error; err != nil {
		t.Fatal(err)
	}
	attachOrderPublishEndpoint(t, base)
	// Actual core PaymentFact top-up, followed by ordinary wallet allocation and
	// fulfillment. No paid source or entitlement evidence is synthesized here.
	fundTestWallet(t, base, balance)
	at := time.Now().UTC().Truncate(time.Millisecond)
	clock := func() time.Time { return at }
	db := base.h.db.Session(&gorm.Session{NowFunc: clock})
	quote := &automaticRecurringQuoter{}
	quote.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		// This succeeds with a single SQLite connection only after the baseline
		// transaction has ended. It also touches the actual scan guard, so an
		// accidentally retained worker writer lock cannot hide behind a read.
		if err := db.WithContext(ctx).Exec("UPDATE automatic_renewal_scans SET sequence=sequence WHERE id=1").Error; err != nil {
			return commerce.DiscountQuote{}, err
		}
		terms := `{"discount_minor":2000,"kind":"recurring-fixture"}`
		hash, _ := commerce.DiscountTermsHash(terms)
		agreement := commerce.DiscountAgreement{ID: "recurring", Version: "1", Scope: "recurring", TermsHash: hash, TermsJSON: terms}
		if in.Agreement != nil {
			agreement = *in.Agreement
		}
		return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: at.Add(time.Minute), Agreement: &agreement}}, nil
	}
	issuer := zeroadapter.Issuer{Cipher: base.h.credentialCipher, Mieru: base.h.zeroMieruAccess}
	f := automaticRecurringFixture{base: base, db: db, at: &at, quotes: quote, orders: commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: db, Now: clock, Discounts: quote}}, payments: commerce.WalletPayments{Repository: commercestore.WalletPayments{DB: db, Now: clock, Issuer: issuer}}, auto: commerce.AutomaticRenewals{Repository: commercestore.AutomaticRenewals{DB: db, Now: clock, Issuer: issuer, Discounts: quote}}}
	f.root = f.buy(t, 0, "automatic-recurring-root")
	if err := db.First(&f.sub, f.root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func (f automaticRecurringFixture) buy(t *testing.T, target uint, key string) model.Order {
	t.Helper()
	r := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: target, IdempotencyKey: key}
	if target == 0 {
		r.Discount = &commerce.OrderDiscountSelection{ProviderID: "example.quote", Selection: "recurring"}
	}
	p, err := f.orders.Preview(t.Context(), 1, r)
	if err != nil {
		t.Fatal(err)
	}
	r.QuoteFingerprint = p.QuoteFingerprint
	o, err := f.orders.Create(t.Context(), 1, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, o.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if p, err := f.payments.Pay(t.Context(), 1, o.ID); err != nil || !p.Fulfilled {
		t.Fatal("trusted recurring purchase", p, err)
	}
	var saved model.Order
	if err := f.db.First(&saved, o.ID).Error; err != nil {
		t.Fatal(err)
	}
	return saved
}

func (f automaticRecurringFixture) enable(t *testing.T) commerce.AutomaticRenewalView {
	t.Helper()
	v, err := f.auto.Read(t.Context(), 1, f.sub.ID, f.base.skuRecord.ID)
	if err != nil || v.Offer == nil || v.Offer.AgreementVersion != 2 || v.Offer.BasePriceMinor != 12000 || v.Offer.DiscountAmountMinor != 2000 || v.Offer.PayableAmountMinor != 10000 || v.Offer.Discount == nil {
		t.Fatal("missing explicit complete recurring offer", v, err)
	}
	calls := f.quotes.calls.Load()
	out, err := f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.base.skuRecord.ID, QuoteFingerprint: v.Offer.QuoteFingerprint, ExpectedRevision: v.Revision})
	if err != nil || !out.Enabled {
		t.Fatal("recurring approval", out, err)
	}
	if f.quotes.calls.Load() != calls+1 {
		t.Fatal("Set reused a read offer without fresh provider evaluation")
	}
	return out
}

func TestAutomaticRecurringSQLiteContinuousRenewalsKeepApprovalAndPaidSource(t *testing.T) {
	assertAutomaticRecurringContinuous(t, newOrderFixture(t))
}
func TestMySQLAutomaticRecurringContinuousRenewalsKeepApprovalAndPaidSource(t *testing.T) {
	assertAutomaticRecurringContinuous(t, newMySQLOrderFixture(t))
}
func assertAutomaticRecurringContinuous(t *testing.T, base orderFixture) {
	f := newAutomaticRecurringFixture(t, base, 50000)
	f.enable(t)
	var approved model.SubscriptionAutomaticRenewal
	if err := f.db.First(&approved, "subscription_id = ?", f.sub.ID).Error; err != nil || approved.AgreementVersion != 2 || approved.ApprovalSnapshot == "" {
		t.Fatal("approval not persisted", err)
	}
	priceForFunding(t, base, 20000)
	previous := f.root
	for i := 1; i <= 2; i++ {
		*f.at = f.sub.EndAt.Add(time.Second)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		n, err := f.auto.Process(ctx, 20)
		cancel()
		if err != nil || n != 1 {
			t.Fatal("automatic recurring debit", i, n, err)
		}
		if n, err := f.auto.Process(t.Context(), 20); err != nil || n != 0 {
			t.Fatal("duplicate automatic debit", n, err)
		}
		var row model.SubscriptionAutomaticRenewal
		if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || !row.Enabled || row.State != "renewed" || row.LastOrderID == nil || row.Revision != approved.Revision || row.AgreementDigest != approved.AgreementDigest || row.ApprovalSnapshot != approved.ApprovalSnapshot {
			t.Fatal("normal source advancement rewrote consent", row.State, err)
		}
		var paid model.Order
		if err := f.db.First(&paid, *row.LastOrderID).Error; err != nil || paid.AmountCents != 12000 || paid.DiscountAmount != 2000 || paid.PayableAmount != 10000 || paid.PaidAmount != 10000 || paid.Channel != "wallet" {
			t.Fatal("recurring debit money", err)
		}
		policy, err := commerce.DecodePurchaseSnapshot(paid.PurchaseSnapshot)
		if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(paid), policy) != nil || policy.Discount.Source.AgreementOrderID != previous.ID {
			t.Fatal("automatic order lost paid source", err)
		}
		var value commerce.EntitlementValueSnapshot
		if err := json.Unmarshal([]byte(paid.EntitlementValueSnapshot), &value); err != nil || !value.PaymentVerified || value.ActualPaidMinor != 10000 || value.OrderID != paid.ID {
			t.Fatal("automatic debit lost real fulfillment evidence", err)
		}
		if err := f.db.First(&f.sub, f.sub.ID).Error; err != nil {
			t.Fatal(err)
		}
		walletMatches(t, base, 40000-int64(i)*10000, 0, 3+i*2)
		previous = paid
	}
}

func TestAutomaticRecurringSQLiteUnavailableCanReadAndWithdrawWithoutRPC(t *testing.T) {
	for _, kind := range []string{"stopped", "timeout", "unavailable", "confirmation"} {
		t.Run(kind, func(t *testing.T) {
			f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
			consent := f.enable(t)
			normal := f.quotes.evaluate
			f.quotes.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
				if kind == "stopped" {
					return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteUnavailable
				}
				if kind == "timeout" {
					return commerce.DiscountQuote{}, context.DeadlineExceeded
				}
				q, err := normal(ctx, in)
				if kind == "confirmation" {
					q.Offer.Status = "requires_confirmation"
					q.Offer.Agreement.Version = "2"
				} else {
					q.Offer.Status = "unavailable"
					q.Offer.DiscountMinor = 0
					q.Offer.Agreement = nil
					q.Offer.Message = "temporarily unavailable"
				}
				return q, err
			}
			v, err := f.auto.Read(t.Context(), 1, f.sub.ID, 0)
			if err != nil || !v.Enabled || v.Revision != consent.Revision || v.Offer != nil || v.Message == "" {
				t.Fatal("provider outage hid revocable consent", v, err)
			}
			*f.at = f.sub.EndAt.Add(time.Second)
			if n, err := f.auto.Process(t.Context(), 20); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			var row model.SubscriptionAutomaticRenewal
			if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || row.LastOrderID != nil || row.Enabled == (kind == "confirmation") || row.State == "renewed" {
				t.Fatal("unavailable/confirmation charged", row.State, err)
			}
			walletMatches(t, f.base, 40000, 0, 3)
			calls := f.quotes.calls.Load()
			if _, err := f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{ExpectedRevision: row.Revision}); err != nil || f.quotes.calls.Load() != calls {
				t.Fatal("withdrawal needed provider", err)
			}
		})
	}
}

func TestAutomaticRecurringSQLiteMaterialChangesRequireNewConsent(t *testing.T) {
	for _, kind := range []string{"discount", "config", "package", "generation", "signer", "traffic", "period", "renewable"} {
		t.Run(kind, func(t *testing.T) {
			f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
			consent := f.enable(t)
			normal := f.quotes.evaluate
			f.quotes.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
				q, err := normal(ctx, in)
				switch kind {
				case "discount":
					q.Offer.DiscountMinor = 1000
				case "config":
					q.Provider.ConfigRevision++
				case "package":
					q.Provider.PackageDigest = strings.Repeat("c", 64)
				case "generation":
					q.Provider.Generation++
				case "signer":
					q.Provider.SigningKeyFingerprint = strings.Repeat("c", 64)
				}
				return q, err
			}
			if kind == "traffic" {
				if err := f.db.Model(&f.base.planRecord).Update("traffic_bytes", 2048).Error; err != nil {
					t.Fatal(err)
				}
			}
			if kind == "period" {
				if err := f.db.Model(&f.base.skuRecord).Update("billing_value", 2).Error; err != nil {
					t.Fatal(err)
				}
			}
			if kind == "renewable" {
				if err := f.db.Model(&f.base.planRecord).Update("is_renewable", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			*f.at = f.sub.EndAt.Add(time.Second)
			if n, err := f.auto.Process(t.Context(), 20); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			var row model.SubscriptionAutomaticRenewal
			if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || row.Enabled || row.Revision != consent.Revision+1 || row.State != "conditions_changed" || row.LastOrderID != nil {
				t.Fatal("material drift kept debit authority", row.State, err)
			}
			walletMatches(t, f.base, 40000, 0, 3)
		})
	}
}

func TestAutomaticRecurringSQLiteRPCWindowRejectsConsentAndSourceAdvancement(t *testing.T) {
	for _, kind := range []string{"withdraw", "manual-renewal", "unsubscribe"} {
		t.Run(kind, func(t *testing.T) {
			f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
			consent := f.enable(t)
			normal := f.quotes.evaluate
			mutated := false
			*f.at = f.sub.EndAt.Add(time.Second)
			f.quotes.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
				q, err := normal(ctx, in)
				if !mutated {
					mutated = true
					if kind == "withdraw" {
						_, err = f.auto.Set(ctx, 1, f.sub.ID, commerce.AutomaticRenewalRequest{ExpectedRevision: consent.Revision})
					} else if kind == "manual-renewal" {
						f.buy(t, f.sub.ID, "manual-during-automatic-rpc")
					} else {
						clock := func() time.Time { return *f.at }
						values := commerce.SubscriptionValues{Repository: commercestore.SubscriptionValues{DB: f.db, Now: clock}}
						v, valueErr := values.Read(ctx, 1, f.sub.ID)
						if valueErr != nil {
							return commerce.DiscountQuote{}, valueErr
						}
						_, err = (commerce.SubscriptionTermination{Repository: commercestore.SubscriptionTermination{DB: f.db, Now: clock}}).Terminate(ctx, 1, f.sub.ID, commerce.SubscriptionTerminationRequest{IdempotencyKey: "unsubscribe-during-recurring-rpc", QuoteFingerprint: v.Fingerprint})
					}
				}
				return q, err
			}
			if n, err := f.auto.Process(t.Context(), 20); err != nil || n != 0 {
				t.Fatal("stale RPC charged advanced source/consent", n, err)
			}
			var row model.SubscriptionAutomaticRenewal
			if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || row.LastOrderID != nil {
				t.Fatal("stale automatic quote committed", err)
			}
			balance, entries := int64(40000), 3
			if kind == "manual-renewal" {
				balance, entries = 30000, 5
			}
			walletMatches(t, f.base, balance, 0, entries)
		})
	}
}

func TestAutomaticRecurringSQLiteDamagedAuthorityIsIsolatedAndRevocable(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "unknown-field", "wrong-digest", "zero-version", "unverified-source"} {
		t.Run(kind, func(t *testing.T) {
			f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
			consent := f.enable(t)
			changes := map[string]any{}
			switch kind {
			case "missing":
				changes["approval_snapshot"] = ""
			case "malformed":
				changes["approval_snapshot"] = "{"
			case "unknown-field":
				changes["approval_snapshot"] = `{"version":2,"future":true}`
			case "wrong-digest":
				changes["agreement_digest"] = strings.Repeat("c", 64)
			case "zero-version":
				changes["agreement_version"] = 0
			case "unverified-source":
				if err := f.db.Model(&f.root).Update("entitlement_value_snapshot", `{}`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if len(changes) > 0 {
				if err := f.db.Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ?", f.sub.ID).Updates(changes).Error; err != nil {
					t.Fatal(err)
				}
			}
			*f.at = f.sub.EndAt.Add(time.Second)
			if n, err := f.auto.Process(t.Context(), 20); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			var row model.SubscriptionAutomaticRenewal
			if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || row.LastOrderID != nil {
				t.Fatal("invalid authority charged", row.State, err)
			}
			if kind != "unverified-source" && (row.Enabled || row.State != "conditions_changed" || row.Revision != consent.Revision+1) {
				t.Fatal("invalid authorization was retained", row.State)
			}
			walletMatches(t, f.base, 40000, 0, 3)
			calls := f.quotes.calls.Load()
			if _, err := f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{ExpectedRevision: row.Revision}); err != nil || f.quotes.calls.Load() != calls {
				t.Fatal("damaged authority blocked withdrawal", err)
			}
		})
	}
}

func TestAutomaticRecurringSQLiteSetRechecksExpiryAfterConsentLock(t *testing.T) {
	f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
	v, err := f.auto.Read(t.Context(), 1, f.sub.ID, f.base.skuRecord.ID)
	if err != nil || v.Offer == nil {
		t.Fatal(v, err)
	}
	type marker struct{}
	const callback = "test:recurring-approval-lock-clock"
	if err := f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if tx.Statement.Context.Value(marker{}) == true && tx.Statement.Table == "subscription_automatic_renewals" && ok && locking.Strength == "UPDATE" {
			*f.at = f.at.Add(2 * time.Minute)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callback)
	_, err = f.auto.Set(context.WithValue(t.Context(), marker{}, true), 1, f.sub.ID, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.base.skuRecord.ID, QuoteFingerprint: v.Offer.QuoteFingerprint, ExpectedRevision: v.Revision})
	if err == nil {
		t.Fatal("expired quote became repeating-debit authority after consent lock")
	}
	var count int64
	if err := f.db.Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ? AND enabled = ?", f.sub.ID, true).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("expired approval persisted", err)
	}
}

func TestAutomaticRecurringSQLiteExpiryBeforeInsertPersistsNoChargeRetry(t *testing.T) {
	f := newAutomaticRecurringFixture(t, newOrderFixture(t), 50000)
	f.enable(t)
	*f.at = f.sub.EndAt.Add(time.Second)
	type marker struct{}
	const callback = "test:automatic-recurring-wallet-lock-clock"
	var advanced atomic.Bool
	if err := f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if tx.Statement.Context.Value(marker{}) == true && tx.Statement.Table == "wallet_accounts" && ok && locking.Strength == "UPDATE" && advanced.CompareAndSwap(false, true) {
			*f.at = f.at.Add(2 * time.Minute)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callback)
	if n, err := f.auto.Process(context.WithValue(t.Context(), marker{}, true), 20); err != nil || n != 1 {
		t.Fatal("expired quote was not isolated as no-charge retry", n, err)
	}
	var row model.SubscriptionAutomaticRenewal
	if err := f.db.First(&row, "subscription_id = ?", f.sub.ID).Error; err != nil || !row.Enabled || row.State != "retry_pending" || row.LastOrderID != nil {
		t.Fatal("expired quote retained partial settlement", row.State, err)
	}
	var pending int64
	if err := f.db.Model(&model.Order{}).Where("target_subscription_id = ?", f.sub.ID).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatal("expired quote created a pending order", pending, err)
	}
	walletMatches(t, f.base, 40000, 0, 3)
}

func TestAutomaticRecurringSQLiteStableWalletCompetition(t *testing.T) {
	assertAutomaticRecurringCompetition(t, newOrderFixture(t))
}
func TestMySQLAutomaticRecurringStableWalletCompetition(t *testing.T) {
	assertAutomaticRecurringCompetition(t, newMySQLOrderFixture(t))
}
func assertAutomaticRecurringCompetition(t *testing.T, base orderFixture) {
	f := newAutomaticRecurringFixture(t, base, 30000)
	f.enable(t)
	second := f.buy(t, 0, "second-automatic-recurring-root")
	other := f
	other.sub = model.Subscription{}
	if err := f.db.First(&other.sub, second.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	other.enable(t)
	*f.at = other.sub.EndAt.Add(time.Second)
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := f.auto.Process(t.Context(), 20); done <- err }()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("stable automatic competition", err)
		}
	}
	var first, last model.SubscriptionAutomaticRenewal
	if err := f.db.First(&first, "subscription_id = ?", f.sub.ID).Error; err != nil || first.State != "renewed" || first.AttemptSequence != 1 {
		t.Fatal("earliest subscription lost balance competition", first.State, err)
	}
	if err := f.db.First(&last, "subscription_id = ?", other.sub.ID).Error; err != nil || last.State != "insufficient_balance" || last.AttemptSequence != 2 || last.LastOrderID != nil {
		t.Fatal("later subscription bypassed stable queue", last.State, err)
	}
	walletMatches(t, base, 0, 0, 7)
}
