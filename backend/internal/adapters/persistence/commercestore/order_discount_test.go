package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type checkoutQuoteStub struct {
	calls    int
	evaluate func(context.Context, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error)
}

func (s *checkoutQuoteStub) Quote(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	s.calls++
	return s.evaluate(ctx, in)
}
func checkoutTestQuote(in commerce.DiscountQuoteContext, now time.Time) commerce.DiscountQuote {
	terms := `{"discount_minor":2000}`
	hash, _ := commerce.DiscountTermsHash(terms)
	return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: now.Add(time.Minute), Agreement: &commerce.DiscountAgreement{ID: "first", Version: "1", Scope: "initial_only", TermsJSON: terms, TermsHash: hash}}}
}

func TestDiscountCheckoutPureRejectsNonAvailableOrUnboundOffers(t *testing.T) {
	in := commerce.DiscountQuoteContext{Version: 1, BuyerID: 1, PlanID: 1, PlanSKUID: 1, Operation: "new", BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, InputFingerprint: strings.Repeat("a", 64)}
	now := time.Now().UTC()
	for _, variant := range []string{"valid", "provider", "recurring", "confirmation", "unavailable", "expired"} {
		t.Run(variant, func(t *testing.T) {
			quote := checkoutTestQuote(in, now)
			switch variant {
			case "provider":
				quote.Provider.PluginID = "other.quote"
			case "recurring":
				quote.Offer.Agreement.Scope = "recurring"
			case "confirmation":
				quote.Offer.Status = "requires_confirmation"
			case "unavailable":
				quote.Offer.Status = "unavailable"
				quote.Offer.DiscountMinor = 0
				quote.Offer.Agreement = nil
				quote.Offer.Message = "offline"
			case "expired":
				quote.Offer.ExpiresAt = now
			}
			err := validateCheckoutOffer(in, quote, "example.quote", now)
			if (err == nil) != (variant == "valid") {
				t.Fatalf("%s: %v", variant, err)
			}
		})
	}
}

func discountSQLiteFixture(t *testing.T) (*gorm.DB, OrderCreation, *checkoutQuoteStub, commerce.OrderCreateRequest) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "checkout.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Plan{}, &model.PlanSKU{}, &model.PlanSKUOperation{}, &model.Order{}, &model.Subscription{}, &model.SystemConfig{}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"ALTER TABLE plans ADD COLUMN archived_at DATETIME", "ALTER TABLE plan_skus ADD COLUMN archived_at DATETIME", "ALTER TABLE orders ADD COLUMN change_snapshot TEXT NOT NULL DEFAULT ''", "ALTER TABLE orders ADD COLUMN entitlement_value_snapshot TEXT NOT NULL DEFAULT ''", "ALTER TABLE subscriptions ADD COLUMN entitlement_epoch INTEGER NOT NULL DEFAULT 0"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []any{
		&model.User{ID: 1, Email: "buyer@example.test", Password: "unused", Status: "active"},
		&model.Plan{ID: 1, Name: "plan", Slug: "plan", NodeGroupID: 1, TrafficBytes: 1024, IsActive: true, IsRenewable: true, DeviceLimit: 1},
		&model.PlanSKU{ID: 1, PlanID: 1, Code: "monthly", Name: "monthly", SKUType: "new", EntitlementMode: "plan", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", PriceCents: 12000, Currency: "CNY", IsActive: true},
		&model.PlanSKUOperation{PlanSKUID: 1, Operation: "purchase"}, &model.PlanSKUOperation{PlanSKUID: 1, Operation: "renew"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	stub := &checkoutQuoteStub{evaluate: func(_ context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		return checkoutTestQuote(in, time.Now().UTC()), nil
	}}
	service := OrderCreation{DB: db, Discounts: stub}
	request := commerce.OrderCreateRequest{PlanSKUID: 1, Discount: &commerce.OrderDiscountSelection{ProviderID: "example.quote", Selection: "first"}, IdempotencyKey: "initial-discount-1"}
	return db, service, stub, request
}

// Run only when the parent schedules the SQLite validation window.
func TestDiscountCheckoutSQLiteNewPurchaseLocksBaseAndIdempotency(t *testing.T) {
	db, service, stub, request := discountSQLiteFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	// With one connection this query can only finish after the short financial
	// transaction releases its connection. No RPC may run under financial locks.
	stub.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		if err := db.WithContext(ctx).Exec("UPDATE plans SET name = name WHERE id = 1").Error; err != nil {
			return commerce.DiscountQuote{}, err
		}
		return checkoutTestQuote(in, time.Now().UTC()), nil
	}
	preview, err := service.Preview(ctx, 1, request)
	if err != nil || preview.BasePriceMinor != 12000 || preview.DiscountAmount != 2000 || preview.PayableAmount != 10000 || preview.QuoteFingerprint == "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	order, err := service.Create(ctx, 1, request)
	if err != nil || order.AmountCents != 12000 || order.DiscountAmount != 2000 || order.PayableAmount != 10000 {
		t.Fatalf("order: %+v %v", order, err)
	}
	var policy commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil || commerce.ValidatePurchaseDiscount(order, policy) != nil || !knownInitialPurchaseDiscount(model.Order(order)) {
		t.Fatal("invalid persisted acceptance")
	}
	t.Logf("actual orders.purchase_snapshot JSON: %d bytes", len(order.PurchaseSnapshot))
	calls := stub.calls
	stub.evaluate = func(context.Context, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteUnavailable
	}
	again, err := service.Create(ctx, 1, request)
	if err != nil || again.ID != order.ID || stub.calls != calls {
		t.Fatal("idempotent retry depended on provider", again.ID, err, stub.calls, calls)
	}
	altered := request
	other := *request.Discount
	other.Selection = "other"
	altered.Discount = &other
	if _, err := service.Create(ctx, 1, altered); err == nil || stub.calls != calls {
		t.Fatal("changed idem request invoked provider", err)
	}
	// Simulate payment facts only to exercise the persisted price-source query.
	// Full fulfillment/payment/automatic-worker acceptance is separately tested.
	now := time.Now().UTC()
	sub := model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, StartAt: now, EndAt: now.AddDate(0, 1, 0), Status: "active", FlowTotal: 1024, ResetQuotaBytes: 1024, RenewalPriceMinor: 12000, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{"status": "paid", "subscription_id": sub.ID, "paid_amount": 10000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PlanSKU{}).Where("id = 1").Update("price_cents", 20000).Error; err != nil {
		t.Fatal(err)
	}
	renewal, err := service.Preview(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID})
	if err != nil || renewal.AmountCents != 12000 || renewal.PayableAmount != 12000 {
		t.Fatalf("renewal borrowed discounted/catalog price: %+v %v", renewal, err)
	}
}

func TestDiscountCheckoutSQLiteRevalidatesAfterProviderAndRollsBack(t *testing.T) {
	for _, mutation := range []string{"price", "policy", "inactive-sku", "buyer", "operation", "capacity", "expiry", "provider-config", "provider-signer", "provider-terms", "provider-discount", "provider-unavailable", "provider-failure", "rollback"} {
		t.Run(mutation, func(t *testing.T) {
			db, service, stub, request := discountSQLiteFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			preview, err := service.Preview(ctx, 1, request)
			if err != nil {
				t.Fatal(err)
			}
			request.QuoteFingerprint = preview.QuoteFingerprint
			stub.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
				var err error
				switch mutation {
				case "price":
					err = db.Model(&model.PlanSKU{}).Where("id = 1").Update("price_cents", 13000).Error
				case "policy":
					err = db.Model(&model.Plan{}).Where("id = 1").Update("traffic_bytes", 2048).Error
				case "inactive-sku":
					err = db.Model(&model.PlanSKU{}).Where("id = 1").Update("is_active", false).Error
				case "buyer":
					err = db.Model(&model.User{}).Where("id = 1").Update("status", "disabled").Error
				case "operation":
					err = db.Where("plan_sku_id = 1 AND operation = ?", "purchase").Delete(&model.PlanSKUOperation{}).Error
				case "capacity":
					err = db.Model(&model.Plan{}).Where("id = 1").Update("max_active_subscriptions", 1).Error
					if err == nil {
						err = db.Create(&model.Subscription{UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "active", EndAt: time.Now().Add(time.Hour), FlowTotal: 1024, Config: "{}"}).Error
					}
				case "rollback":
					err = db.Callback().Create().After("gorm:create").Register("discount-insert-rollback", func(tx *gorm.DB) {
						if tx.Statement.Table == "orders" {
							tx.AddError(errors.New("injected insert rollback"))
						}
					})
				}
				quote := checkoutTestQuote(in, time.Now().UTC())
				switch mutation {
				case "expiry":
					quote.Offer.ExpiresAt = time.Now().Add(-time.Second)
				case "provider-config":
					quote.Provider.ConfigRevision++
				case "provider-signer":
					quote.Provider.SigningKeyFingerprint = strings.Repeat("c", 64)
				case "provider-terms":
					quote.Offer.Agreement.Version = "2"
				case "provider-discount":
					quote.Offer.DiscountMinor = 1000
				case "provider-unavailable":
					quote.Offer.Status = "unavailable"
					quote.Offer.DiscountMinor = 0
					quote.Offer.Agreement = nil
					quote.Offer.Message = "offline"
				case "provider-failure":
					err = commerce.ErrDiscountQuoteUnavailable
				}
				return quote, err
			}
			out, err := service.Create(ctx, 1, request)
			if err == nil || out.ID != 0 {
				t.Fatalf("changed facts/rollback accepted: %+v %v", out, err)
			}
			var count int64
			if err := db.Model(&model.Order{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed create left order: %d %v", count, err)
			}
		})
	}
}

func TestDiscountCheckoutSQLitePersistsLongTermsWithoutTruncation(t *testing.T) {
	_, service, stub, request := discountSQLiteFixture(t)
	terms := `{"description":"` + strings.Repeat("x", 7900) + `"}`
	stub.evaluate = func(_ context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		quote := checkoutTestQuote(in, time.Now().UTC())
		quote.Offer.Agreement.TermsJSON = terms
		quote.Offer.Agreement.TermsHash, _ = commerce.DiscountTermsHash(terms)
		return quote, nil
	}
	preview, err := service.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	out, err := service.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := commerce.DecodePurchaseSnapshot(out.PurchaseSnapshot)
	if err != nil || policy.Discount.Quote.Offer.Agreement.TermsJSON != terms || !knownInitialPurchaseDiscount(model.Order(out)) {
		t.Fatal("terms lost in snapshot", err)
	}
	if len(out.PurchaseSnapshot) <= 2048 {
		t.Fatal("fixture did not exceed old schema")
	}
	t.Logf("long-terms orders.purchase_snapshot JSON: %d bytes", len(out.PurchaseSnapshot))
}

func TestDiscountCheckoutSQLiteUnknownHistoryAndLateExpiryStayClosed(t *testing.T) {
	_, service, _, request := discountSQLiteFixture(t)
	preview, err := service.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	order, err := service.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"unknown-field", "null-discount", "missing-discount", "missing-base", "changed-payable", "recurring", "malformed"} {
		t.Run(variant, func(t *testing.T) {
			changed := model.Order(order)
			var fields map[string]json.RawMessage
			if json.Unmarshal([]byte(changed.PurchaseSnapshot), &fields) != nil {
				t.Fatal("bad fixture")
			}
			switch variant {
			case "unknown-field":
				fields["payment_authority"] = json.RawMessage(`true`)
			case "null-discount":
				fields["discount"] = json.RawMessage(`null`)
			case "missing-discount":
				delete(fields, "discount")
			case "missing-base":
				delete(fields, "base_price_minor")
			case "changed-payable":
				changed.PayableAmount = 12000
			case "recurring":
				fields["discount"] = json.RawMessage(strings.Replace(string(fields["discount"]), `"scope":"initial_only"`, `"scope":"recurring"`, 1))
			}
			raw, _ := json.Marshal(fields)
			changed.PurchaseSnapshot = string(raw)
			if variant == "malformed" {
				changed.PurchaseSnapshot = `{"version":1}`
			}
			if knownInitialPurchaseDiscount(changed) {
				t.Fatal("unknown/corrupt discount authorized base-price automatic renewal")
			}
		})
	}
	policy, _ := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	accepted := checkoutDiscount{Context: policy.Discount.Context, Quote: policy.Discount.Quote, AcceptedAt: policy.Discount.AcceptedAt}
	changed := model.Order(order)
	if err := refreshDiscountAcceptance(&changed, accepted, accepted.Quote.Offer.ExpiresAt); err == nil {
		t.Fatal("offer expired while waiting for locks accepted")
	}
	// New creates require explicit confirmation, even though preview is safe.
	request.IdempotencyKey = "unconfirmed-purchase"
	request.QuoteFingerprint = ""
	if out, err := service.Create(t.Context(), 1, request); err == nil || out.ID != 0 {
		t.Fatal("discounted order created without accepted preview", err)
	}
}
