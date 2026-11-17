package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestRecurringSourceLockErrorClassificationIsExact(t *testing.T) {
	busy := fmt.Errorf("wrapped: %w", &mysql.MySQLError{Number: 3572, Message: "NOWAIT"})
	if !errors.Is(discountSourceReadError(busy), commerce.ErrSubscriptionQuoteChanged) {
		t.Fatal("3572 not retryable")
	}
	for _, err := range []error{nil, &mysql.MySQLError{Number: 1213, Message: "deadlock"}, &mysql.MySQLError{Number: 1205, Message: "timeout"}, errors.New("Error 3572 NOWAIT")} {
		if discountSourceReadError(err) != err {
			t.Fatal("unrelated failure reclassified", err)
		}
	}
}

// Synthetic paid/entitlement evidence is explicit fixture setup; these tests
// validate repository behavior, not real payment or InnoDB RR concurrency.
func recurringSQLiteFixture(t *testing.T) (*gorm.DB, OrderCreation, *checkoutQuoteStub, model.Order, model.Subscription) {
	db, service, stub, request := discountSQLiteFixture(t)
	stub.evaluate = func(ctx context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		if err := db.WithContext(ctx).Exec("UPDATE plans SET name = name WHERE id = 1").Error; err != nil {
			return commerce.DiscountQuote{}, err
		}
		q := checkoutTestQuote(in, time.Now().UTC())
		q.Offer.Agreement.Scope = "recurring"
		return q, nil
	}
	preview, err := service.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	created, err := service.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := commerce.DecodePurchaseSnapshot(created.PurchaseSnapshot)
	if err != nil || policy.Discount.Version != 2 || commerce.ValidatePurchaseDiscount(created, policy) != nil {
		t.Fatal("invalid V2 new evidence", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	sub := model.Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, StartAt: now, EndAt: now.AddDate(0, 1, 0), Status: "active", FlowTotal: 1024, ResetQuotaBytes: 1024, RenewalPriceMinor: 12000, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	root := model.Order(created)
	makeSyntheticRecurringPaid(t, db, &root, sub)
	return db, service, stub, root, sub
}

func makeSyntheticRecurringPaid(t *testing.T, db *gorm.DB, order *model.Order, sub model.Subscription) {
	t.Helper()
	evidence := commerce.EntitlementValueSnapshot{Version: 1, OrderID: order.ID, SubscriptionID: sub.ID, Currency: order.Currency, ActualPaidMinor: order.PayableAmount, PaymentVerified: true}
	raw, _ := json.Marshal(evidence)
	if err := db.Model(order).Updates(map[string]any{"status": "paid", "subscription_id": sub.ID, "paid_amount": order.PayableAmount, "entitlement_value_snapshot": string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(order, order.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRecurringSQLiteManualRenewalInheritsPaidSourceAndNeverFallsBack(t *testing.T) {
	db, service, stub, root, sub := recurringSQLiteFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := db.Model(&model.PlanSKU{}).Where("id = 1").Update("price_cents", 20000).Error; err != nil {
		t.Fatal(err)
	}
	// Omission deliberately inherits the stored provider and selection.
	request := commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID, IdempotencyKey: "recurring-renew-1"}
	preview, err := service.Preview(ctx, 1, request)
	if err != nil || preview.BasePriceMinor != 12000 || preview.DiscountAmount != 2000 || preview.PayableAmount != 10000 {
		t.Fatalf("renewal reset promotional payable: %+v %v", preview, err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	created, err := service.Create(ctx, 1, request)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := commerce.DecodePurchaseSnapshot(created.PurchaseSnapshot)
	if err != nil || policy.Discount.Version != 2 || policy.Discount.Source.RootOrderID != root.ID || policy.Discount.Source.AgreementOrderID != root.ID || policy.Discount.Context.PriceSourceOrderID != root.ID || policy.Discount.Context.Agreement == nil {
		t.Fatal("wrong source", err)
	}
	calls := stub.calls
	stub.evaluate = func(context.Context, commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteUnavailable
	}
	again, err := service.Create(ctx, 1, request)
	if err != nil || again.ID != created.ID || calls != stub.calls {
		t.Fatal("idempotent recurring retry called unavailable provider", err)
	}
	noProvider := service
	noProvider.Discounts = nil
	if _, err := noProvider.Preview(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}); !errors.Is(err, commerce.ErrDiscountQuoteUnavailable) {
		t.Fatal("omission enabled full-price fallback", err)
	}
	fake := commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID, Discount: &commerce.OrderDiscountSelection{ProviderID: "other.quote"}}
	if _, err := service.Preview(ctx, 1, fake); err == nil || calls != stub.calls {
		t.Fatal("browser replaced provider", err)
	}
	// A pending order never becomes the source. Once paid, strict adjacency moves
	// the source to it while the purchased base remains the original root.
	makeSyntheticRecurringPaid(t, db, (*model.Order)(&created), sub)
	stub.evaluate = func(_ context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		q := checkoutTestQuote(in, time.Now().UTC())
		q.Offer.Agreement.Scope = "recurring"
		return q, nil
	}
	next, err := service.Preview(ctx, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID})
	if err != nil || next.PayableAmount != 10000 {
		t.Fatal(next, err)
	}
}

func TestRecurringSQLiteRejectsSourceChangeAndUnknownDiscount(t *testing.T) {
	for _, variant := range []string{"refund", "unverified", "unknown-row", "source-replaced", "target-epoch", "signer", "confirmation", "unavailable"} {
		t.Run(variant, func(t *testing.T) {
			db, service, stub, root, sub := recurringSQLiteFixture(t)
			request := commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID, IdempotencyKey: "changed-source-1"}
			preview, err := service.Preview(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			request.QuoteFingerprint = preview.QuoteFingerprint
			stub.evaluate = func(_ context.Context, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
				q := checkoutTestQuote(in, time.Now().UTC())
				q.Offer.Agreement.Scope = "recurring"
				var err error
				switch variant {
				case "refund":
					err = db.Model(&root).Update("refund_amount", 1).Error
				case "unverified":
					err = db.Model(&root).Update("entitlement_value_snapshot", `{}`).Error
				case "unknown-row", "source-replaced":
					row := root
					row.ID = 0
					row.TradeNo = "unexpected-paid-source"
					row.CreationKey = nil
					row.CreationFingerprint = ""
					row.OrderType = "renewal"
					row.TargetSubscriptionID = &sub.ID
					row.PurchaseSnapshot = ""
					row.DiscountAmount = 2000
					err = db.Create(&row).Error
				case "target-epoch":
					err = db.Model(&sub).Update("entitlement_epoch", 1).Error
				case "signer":
					q.Provider.SigningKeyFingerprint = strings.Repeat("c", 64)
				case "confirmation":
					q.Offer.Status = "requires_confirmation"
					q.Offer.Agreement.Version = "2"
				case "unavailable":
					q.Offer.Status = "unavailable"
					q.Offer.DiscountMinor = 0
					q.Offer.Agreement = nil
					q.Offer.Message = "unavailable"
				}
				return q, err
			}
			out, err := service.Create(t.Context(), 1, request)
			if err == nil || out.ID != 0 {
				t.Fatal("changed source created order", out.ID, err)
			}
			if variant == "confirmation" && !errors.Is(err, commerce.ErrDiscountConfirmationRequired) {
				t.Fatal("confirmation status lost", err)
			}
			var count int64
			if err := db.Model(&model.Order{}).Where("creation_fingerprint <> ?", "").Count(&count).Error; err != nil || count != 1 {
				t.Fatal("failed renewal retained order", count, err)
			}
		})
	}
}

func TestRecurringSQLitePreselectionChangeNeverLocksNewSourceAfterOwner(t *testing.T) {
	db, service, _, root, sub := recurringSQLiteFixture(t)
	request := commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}
	scope, err := prepareDiscountSourceScope(db, 1, request)
	if err != nil {
		t.Fatal(err)
	}
	unexpected := root
	unexpected.ID = 0
	unexpected.CreationKey = nil
	unexpected.CreationFingerprint = ""
	unexpected.TradeNo = "late-source"
	unexpected.OrderType = "renewal"
	unexpected.TargetSubscriptionID = &sub.ID
	if err := db.Create(&unexpected).Error; err != nil {
		t.Fatal(err)
	}
	extraLocks := 0
	if err := db.Callback().Query().Before("gorm:query").Register("recurring-no-late-source-lock", func(tx *gorm.DB) {
		if tx.Statement.Table == "orders" {
			if _, locking := tx.Statement.Clauses["FOR"]; locking && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), fmt.Sprint(unexpected.ID)) {
				extraLocks++
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("recurring-no-late-source-lock")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockPreparedDiscountSources(tx, scope); err != nil {
			return err
		}
		_, _, err := prepareOrderWithDiscount(tx, 1, request, commerceNow(service.Now), &checkoutDiscount{Scope: scope, Baseline: true})
		return err
	})
	if !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) || extraLocks != 0 {
		t.Fatal("new source was accepted/waited under owner", err, extraLocks)
	}
}

func TestRecurringSQLiteFinancialTransactionsLockSourcesBeforeOwnerAndSnapshot(t *testing.T) {
	db, service, _, _, sub := recurringSQLiteFixture(t)
	type observed struct{ owner, source, consistent bool }
	states := map[string]*observed{}
	violations := []string{}
	if err := db.Callback().Query().Before("gorm:query").Register("recurring-lock-order", func(tx *gorm.DB) {
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
			return
		}
		key := fmt.Sprintf("%p", tx.Statement.ConnPool)
		state := states[key]
		if state == nil {
			state = &observed{}
			states[key] = state
		}
		raw, locked := tx.Statement.Clauses["FOR"]
		lock, _ := raw.Expression.(clause.Locking)
		if tx.Statement.Table == "orders" && locked && lock.Strength == "UPDATE" {
			if state.owner || state.consistent {
				violations = append(violations, "source lock after owner/snapshot")
			}
			state.source = true
			return
		}
		if tx.Statement.Table == "users" && locked && lock.Strength == "UPDATE" {
			if !state.source {
				violations = append(violations, "owner acquired before selected source PKs")
			}
			state.owner = true
			return
		}
		if !locked {
			if !state.owner {
				violations = append(violations, "consistent SELECT before owner")
			}
			state.consistent = true
		}
		if tx.Statement.Table == "orders" && locked && state.owner && lock.Options != "NOWAIT" {
			violations = append(violations, "blocking source current read after owner")
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("recurring-lock-order")
	if _, err := service.Preview(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}); err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || len(violations) != 0 {
		t.Fatal("financial snapshot/lock ordering", len(states), violations)
	}
}

func TestInitialOnlyCheckoutDoesNotLockPaidSourcesBehindLifecycleOwnerWait(t *testing.T) {
	db, service, _, root, sub := recurringSQLiteFixture(t)
	// Explicit synthetic full-price history; unlike a real V1 discount this
	// fixture has no reduction or provider agreement to inherit.
	policy, err := commerce.DecodePurchaseSnapshot(root.PurchaseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	policy.Discount = nil
	raw, _ := json.Marshal(policy)
	if err := db.Model(&root).Updates(map[string]any{"purchase_snapshot": string(raw), "discount_amount": 0, "payable_amount": root.AmountCents, "paid_amount": root.AmountCents}).Error; err != nil {
		t.Fatal(err)
	}
	locks := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test:legacy-source-locks", func(tx *gorm.DB) {
		if tx.Statement.Table == "orders" {
			if _, ok := tx.Statement.Clauses["FOR"]; ok {
				locks++
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:legacy-source-locks")
	request := commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}
	preview, err := service.Preview(t.Context(), 1, request)
	if err != nil || preview.PayableAmount != root.AmountCents || locks != 0 {
		t.Fatal("ordinary renewal retained recurring-only source locks", preview.PayableAmount, locks, err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	if _, err := service.Create(t.Context(), 1, request); err != nil || locks != 0 {
		t.Fatal("ordinary checkout cannot retain lifecycle source locks", locks, err)
	}
}

func TestRecurringSQLiteInternalAutomaticPathAndBrokenSeriesStayClosed(t *testing.T) {
	db, _, _, root, sub := recurringSQLiteFixture(t)
	var invalid *commerce.ValidationError
	err := db.Transaction(func(tx *gorm.DB) error {
		_, _, err := prepareOrder(tx, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}, time.Now().UTC())
		return err
	})
	if !errors.As(err, &invalid) {
		t.Fatal("recurring silently entered automatic full-price prepare", err)
	}
	if knownInitialPurchaseDiscount(root) {
		t.Fatal("recurring became an initial-only automatic permission")
	}
	unknown := root
	unknown.ID = 0
	unknown.CreationKey = nil
	unknown.CreationFingerprint = ""
	unknown.TradeNo = "unknown-discount"
	unknown.OrderType = "renewal"
	unknown.TargetSubscriptionID = &sub.ID
	unknown.PurchaseSnapshot = ""
	if err := db.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	scope, err := prepareDiscountSourceScope(db, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockPreparedDiscountSources(tx, scope); err != nil {
			return err
		}
		_, _, err := prepareOrderWithDiscount(tx, 1, commerce.OrderCreateRequest{PlanSKUID: 1, TargetSubscriptionID: sub.ID}, time.Now().UTC(), &checkoutDiscount{Scope: scope, Baseline: true})
		return err
	})
	if err == nil {
		t.Fatal("unknown monetary discount disappeared from the paid chain")
	}
}
