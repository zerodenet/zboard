package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const largeEvidenceSourceCount = 1200

// The initial purchase and its first 20 used bytes use the real funded-wallet
// and metering paths. The additional rows are explicitly synthetic *captured*
// channel-payment/entitlement facts for a persistence scale fixture, not 1199
// calls to a gateway and not a production historical reconstruction. Each has
// matching receipt, quota-event identity, original paid amount, and FIFO state.
func largeCapturedPurchaseFixture(t *testing.T, base orderFixture) (orderFixture, model.Order, []model.Order) {
	t.Helper()
	f, root := terminationHistoryFixture(t, base)
	var original commerce.EntitlementValueSnapshot
	if err := json.Unmarshal([]byte(root.EntitlementValueSnapshot), &original); err != nil || !original.PaymentVerified {
		t.Fatalf("initial real payment evidence missing: %v", err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	if at.Before(original.CapturedAt) {
		at = original.CapturedAt.Add(time.Millisecond).Truncate(time.Millisecond)
	}
	rows := make([]model.Order, largeEvidenceSourceCount-1)
	for i := range rows {
		target := sub.ID
		rows[i] = model.Order{UserID: root.UserID, SubscriptionID: sub.ID, TargetSubscriptionID: &target,
			PlanID: root.PlanID, PlanSKUID: root.PlanSKUID, TradeNo: fmt.Sprintf("captured-large-%04d", i),
			OrderType: "renewal", Status: "paid", AmountCents: root.AmountCents, PayableAmount: 1, PaidAmount: 1, DiscountAmount: root.AmountCents - 1,
			Currency: root.Currency, Channel: "large-captured-fixture", BillingUnit: "once", BillingValue: 1,
			RenewalEffect: "add_quota_only", TrafficBytes: 1, PurchaseSnapshot: root.PurchaseSnapshot,
			AssignedBy: 1, AssignmentNote: "captured administrator price override; only one actual paid minor unit",
			PaidAt: &at, FulfilledAt: &at, CreatedAt: at, UpdatedAt: at}
	}
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
		events := make([]model.QuotaEvent, len(rows))
		receipts := make([]model.PaymentReceipt, len(rows))
		for i, order := range rows {
			key := sha256.Sum256([]byte("captured-large-receipt:" + strconv.FormatUint(uint64(order.ID), 10)))
			events[i] = model.QuotaEvent{SubscriptionID: sub.ID, EntitlementEpoch: sub.EntitlementEpoch,
				EventType: "renewal", DeltaBytes: 1, BalanceBefore: sub.FlowTotal + int64(i) - sub.FlowUsed,
				BalanceAfter: sub.FlowTotal + int64(i) + 1 - sub.FlowUsed, ReferenceType: "order",
				ReferenceID: strconv.FormatUint(uint64(order.ID), 10), Detail: "{}", CreatedAt: at}
			receipts[i] = model.PaymentReceipt{ReceiptKey: hex.EncodeToString(key[:]), OrderID: order.ID,
				Provider: "large-captured-fixture", ProviderTradeNo: order.TradeNo, AmountMinor: 1,
				Currency: root.Currency, Disposition: "fulfilled", CreatedAt: at, ProcessedAt: at}
		}
		if err := tx.CreateInBatches(&events, 100).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(&receipts, 100).Error; err != nil {
			return err
		}
		before := original.After
		before.FlowUsed = sub.FlowUsed
		for i := range rows {
			after := before
			after.FlowTotal++
			after.ResetQuotaBytes = 1
			fact := commerce.EntitlementValueSnapshot{Version: 1, OrderID: rows[i].ID, SubscriptionID: sub.ID,
				QuotaEventID: events[i].ID, QuotaEventAt: at, CapturedAt: at, Currency: root.Currency,
				ActualPaidMinor: 1, PaymentVerified: true, PaymentBasis: "verified_cash_or_wallet",
				Kind: "renewal", Mode: "perpetual_quota_addition", AllocationModel: commerce.PerpetualQuotaFIFOAllocation,
				BillingUnit: "once", BillingValue: 1, TermStartAt: at, TermEndAt: after.EndAt, Before: &before, After: after}
			encoded, err := json.Marshal(fact)
			if err != nil {
				return err
			}
			rows[i].EntitlementValueSnapshot = string(encoded)
			before = after
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"entitlement_value_snapshot"})}).CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
		return tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"flow_total": before.FlowTotal, "reset_quota_bytes": before.ResetQuotaBytes}).Error
	}); err != nil {
		t.Fatal(err)
	}
	return f, root, rows
}

// Observe returned batches rather than asserting an implementation's SQL text.
// The assertions cover metadata, exact-ID lock and text-loading queries alike.
func guardLargeEvidencePages(t *testing.T, db *gorm.DB) (*atomic.Int32, func()) {
	t.Helper()
	var batches atomic.Int32
	const name = "test:large_evidence_page_bound"
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" && tx.Statement.Table != "order_value_budgets" {
			return
		}
		value := reflect.ValueOf(tx.Statement.Dest)
		if value.Kind() == reflect.Pointer && !value.IsNil() {
			value = value.Elem()
		}
		if value.Kind() == reflect.Slice {
			batches.Add(1)
			if value.Len() > 200 {
				tx.AddError(fmt.Errorf("order evidence query returned %d rows, limit is 200", value.Len()))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	remove := func() { once.Do(func() { db.Callback().Query().Remove(name) }) }
	t.Cleanup(remove)
	return &batches, remove
}

func largeEvidenceSnapshotDigest(t *testing.T, f orderFixture, subscription uint) [32]byte {
	t.Helper()
	var rows []struct {
		ID                       uint
		PaidAmount               int64
		EntitlementValueSnapshot string
	}
	if err := f.h.db.Model(&model.Order{}).Select("id,paid_amount,entitlement_value_snapshot").Where("subscription_id = ? AND status = ?", subscription, "paid").Order("id ASC").Find(&rows).Error; err != nil || len(rows) != largeEvidenceSourceCount {
		t.Fatalf("captured source count=%d err=%v", len(rows), err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

func TestLargePurchaseEvidenceWholeTermination(t *testing.T) {
	assertLargePurchaseTermination(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}

func TestMySQLLargePurchaseEvidenceWholeTermination(t *testing.T) {
	assertLargePurchaseTermination(t, newMySQLBusinessOrderFixture)
}

func assertLargePurchaseTermination(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%t", unknown), func(t *testing.T) {
			f, root, rows := largeCapturedPurchaseFixture(t, fixture(t))
			if unknown {
				var fact commerce.EntitlementValueSnapshot
				if err := json.Unmarshal([]byte(rows[1100].EntitlementValueSnapshot), &fact); err != nil {
					t.Fatal(err)
				}
				fact.PaymentVerified = false // A paid status alone must never become cash.
				raw, _ := json.Marshal(fact)
				if err := f.h.db.Model(&model.Order{}).Where("id = ?", rows[1100].ID).Update("entitlement_value_snapshot", string(raw)).Error; err != nil {
					t.Fatal(err)
				}
			}
			before := largeEvidenceSnapshotDigest(t, f, root.SubscriptionID)
			batches, remove := guardLargeEvidencePages(t, f.h.db)
			quote, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, root.SubscriptionID)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(8000 + largeEvidenceSourceCount - 1)
			var receiptID uint
			if unknown {
				if quote.ValuationStatus != "unavailable" || quote.TotalMinor != nil || quote.MaxRefundMinor != 0 {
					t.Fatalf("unknown current source became quoted money %+v", quote)
				}
				if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
					t.Fatalf("unverified paid row accepted: %v", err)
				}
				positive := int64(1)
				in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "large-current-unpriced-zero"}, AmountMinor: &positive, Reason: "explicit zero termination despite missing captured proof"}
				if _, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, root.SubscriptionID, in); !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
					t.Fatalf("unknown cash paid a positive refund: %v", err)
				}
				zero := int64(0)
				in.AmountMinor = &zero
				for range 2 {
					receipt, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, root.SubscriptionID, in)
					if err != nil || receipt.AmountMinor != 0 || receipt.WalletEntryID != nil || receipt.RecordKind != "entitlement_mutation" || receiptID != 0 && receipt.ID != receiptID {
						t.Fatalf("unpriced whole termination %+v %v", receipt, err)
					}
					receiptID = receipt.ID
				}
				want = 0
			} else {
				if quote.ValuationStatus != "verified" || quote.TotalMinor == nil || *quote.TotalMinor != want || quote.MaxRefundMinor != 10000+largeEvidenceSourceCount-1 {
					t.Fatalf("large administrator price/actual-paid limits %+v", quote)
				}
				value, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
				if err != nil || value.TotalMinor != want || value.FutureMinor != 0 {
					t.Fatalf("large FIFO current residual %+v %v", value, err)
				}
				in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: value.Fingerprint, IdempotencyKey: "large-current-whole-refund"}
				for range 2 {
					receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, in)
					if err != nil || receipt.AmountMinor != want || receiptID != 0 && receipt.ID != receiptID {
						t.Fatalf("large whole termination replay %+v %v", receipt, err)
					}
					receiptID = receipt.ID
				}
				in.IdempotencyKey = "large-current-second-refund"
				if _, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, in); !errors.Is(err, commerce.ErrSubscriptionSettlementConflict) {
					t.Fatalf("large source value returned twice: %v", err)
				}
			}
			remove()
			if batches.Load() < 6 || receiptID == 0 {
				t.Fatalf("large source operation did not traverse bounded pages: %d", batches.Load())
			}
			if before != largeEvidenceSnapshotDigest(t, f, root.SubscriptionID) {
				t.Fatal("historical paid prices or captured entitlement evidence were rewritten")
			}
			var wallet model.WalletAccount
			if err := f.h.db.Where("user_id = ? AND currency = ?", 1, root.Currency).First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want || wallet.ReservedMinor != 0 {
				t.Fatalf("large refund wallet %+v %v", wallet, err)
			}
			var sub model.Subscription
			if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil || sub.FlowUsed != 20 {
				t.Fatalf("remaining rights or usage changed incorrectly %+v %v", sub, err)
			}
			var refunds int64
			if err := f.h.db.Model(&model.WalletEntry{}).Where("kind = ?", "subscription_refund").Count(&refunds).Error; err != nil || want == 0 && refunds != 0 || want > 0 && refunds != 1 {
				t.Fatalf("duplicate/zero-money ledger count=%d %v", refunds, err)
			}
			var cashReceipts int64
			if err := f.h.db.Model(&model.PaymentReceipt{}).Where("provider = ?", "large-captured-fixture").Count(&cashReceipts).Error; err != nil || cashReceipts != largeEvidenceSourceCount-1 {
				t.Fatalf("captured channel receipts lost count=%d %v", cashReceipts, err)
			}
		})
	}
}

func TestLargePurchaseEvidenceFullReplacement(t *testing.T) {
	assertLargePurchaseReplacement(t, newOrderFixture(t))
}

func TestMySQLLargePurchaseEvidenceFullReplacement(t *testing.T) {
	assertLargePurchaseReplacement(t, newMySQLBusinessOrderFixture(t))
}

func assertLargePurchaseReplacement(t *testing.T, base orderFixture) {
	t.Helper()
	f, root, _ := largeCapturedPurchaseFixture(t, base)
	before := largeEvidenceSnapshotDigest(t, f, root.SubscriptionID)
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 2000, "change")
	batches, remove := guardLargeEvidencePages(t, f.h.db)
	in := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "large-current-full-replacement"}
	preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, in)
	want := int64(8000 + largeEvidenceSourceCount - 1)
	if err != nil || preview.SettlementAmount != want || preview.SurplusAmount != want-2000 || preview.AdditionalAmount != 0 {
		t.Fatalf("large replacement quote %+v %v", preview, err)
	}
	in.QuoteFingerprint = preview.QuoteFingerprint
	order, err := f.h.services.OrderCreation.Create(t.Context(), 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := fundingService(f).Pay(t.Context(), 1, order.ID); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	if batches.Load() < 6 {
		t.Fatal("replacement did not traverse its source pages")
	}
	// The new paid order is excluded: old immutable evidence itself is unchanged.
	var old []model.Order
	if err := f.h.db.Where("subscription_id = ? AND id <> ?", root.SubscriptionID, order.ID).Order("id").Find(&old).Error; err != nil || len(old) != largeEvidenceSourceCount {
		t.Fatalf("replacement source count=%d %v", len(old), err)
	}
	var original []struct {
		ID                       uint
		PaidAmount               int64
		EntitlementValueSnapshot string
	}
	for _, row := range old {
		original = append(original, struct {
			ID                       uint
			PaidAmount               int64
			EntitlementValueSnapshot string
		}{row.ID, row.PaidAmount, row.EntitlementValueSnapshot})
	}
	raw, _ := json.Marshal(original)
	if sha256.Sum256(raw) != before {
		t.Fatal("replacement rewrote original source evidence")
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, root.Currency).First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want-2000 || wallet.ReservedMinor != 0 {
		t.Fatalf("large replacement wallet %+v %v", wallet, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.PlanID != plan.ID || sub.FlowUsed != 20 || sub.EntitlementEpoch != 1 {
		t.Fatalf("old rights carried into replacement %+v %v", sub, err)
	}
	if total, used := entitlements.CycleQuota(entitlements.Subscription(sub)); total != 100 || used != 0 {
		t.Fatalf("replacement quota carried old remaining value: total=%d used=%d", total, used)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id = ? AND kind = ?", sub.ID, "change").Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatalf("replacement settled twice count=%d %v", settlements, err)
	}
}

func TestLargePurchaseEvidenceByteBudgetRejectsBeforeBodyLoad(t *testing.T) {
	assertLargePurchaseByteBudget(t, newOrderFixture(t))
}

func TestMySQLLargePurchaseEvidenceByteBudgetRejectsBeforeBodyLoad(t *testing.T) {
	assertLargePurchaseByteBudget(t, newMySQLBusinessOrderFixture(t))
}

func assertLargePurchaseByteBudget(t *testing.T, base orderFixture) {
	t.Helper()
	f, root := terminationHistoryFixture(t, base)
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	// Below 1Mi characters but above 1Mi bytes: SQLite must measure BLOB bytes,
	// matching MySQL OCTET_LENGTH, before allocating this irrelevant old text.
	if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("change_snapshot", strings.Repeat("界", 350000)).Error; err != nil {
		t.Fatal(err)
	}
	const guard = "test:large_evidence_no_body_after_over_budget"
	var bodyLoads atomic.Int32
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(guard, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" {
			return
		}
		if _, ok := tx.Statement.Dest.(*[]model.Order); ok && strings.Join(tx.Statement.Selects, ",") != "id" {
			bodyLoads.Add(1)
			tx.AddError(errors.New("over-budget evidence body was loaded"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	remove := func() { f.h.db.Callback().Query().Remove(guard) }
	if _, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID); !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
		remove()
		t.Fatalf("oversized source returned a partial quote: %v", err)
	}
	_, err = f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "oversized-evidence-no-refund"})
	remove()
	if !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) || bodyLoads.Load() != 0 {
		t.Fatalf("oversized source was loaded or settled: body=%d err=%v", bodyLoads.Load(), err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil || sub.FlowUsed != 20 {
		t.Fatalf("budget rejection changed rights %+v %v", sub, err)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, root.Currency).First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000 || wallet.ReservedMinor != 0 {
		t.Fatalf("budget rejection changed wallet %+v %v", wallet, err)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements != 0 {
		t.Fatalf("partial over-budget settlement count=%d %v", settlements, err)
	}
}

func TestLargePurchaseEvidenceRechecksCrossPageMembership(t *testing.T) {
	assertLargePurchaseScopeChange(t, newOrderFixture(t))
}

func TestMySQLLargePurchaseEvidenceRechecksCrossPageMembership(t *testing.T) {
	assertLargePurchaseScopeChange(t, newMySQLBusinessOrderFixture(t))
}

func assertLargePurchaseScopeChange(t *testing.T, base orderFixture) {
	t.Helper()
	f, root, _ := largeCapturedPurchaseFixture(t, base)
	quote, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	defer release()
	var paused atomic.Bool
	const guard = "test:large_evidence_last_unlocked_page"
	if err := f.h.db.Callback().Query().After("gorm:query").Register(guard, func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" || !strings.Contains(strings.Join(tx.Statement.Selects, " "), "evidence_bytes") {
			return
		}
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
			return
		}
		value := reflect.ValueOf(tx.Statement.Dest)
		if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Slice || value.Elem().Len() != 0 || !paused.CompareAndSwap(false, true) {
			return
		}
		close(prepared)
		<-resume
	}); err != nil {
		t.Fatal(err)
	}
	defer f.h.db.Callback().Query().Remove(guard)
	done := make(chan error, 1)
	in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: quote.Fingerprint, IdempotencyKey: "large-cross-page-membership"}
	go func() {
		_, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, in)
		done <- err
	}()
	select {
	case <-prepared:
	case err := <-done:
		t.Fatalf("cross-page preparation did not pause: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("last unlocked preparation page not reached")
	}
	// Use a real different-plan checkout: these captured administrator reductions
	// are not a recurring discount contract and must not enable silent renewal.
	plan := f.plan(t, 2)
	sku := f.sku(t, plan.ID, 20000, "change")
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "large-scope-new-reservation"}
	preview, err := f.h.services.OrderCreation.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	pending, err := f.h.services.OrderCreation.Create(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-done; !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("new cross-page source membership was missed: %v", err)
	}
	var fund model.OrderFunding
	if err := f.h.db.First(&fund, "order_id = ?", pending.ID).Error; err != nil || fund.State != "reserved" {
		t.Fatalf("scope retry consumed reservation %+v %v", fund, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
		t.Fatalf("scope retry terminated rights %+v %v", sub, err)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements != 0 {
		t.Fatalf("scope retry committed value count=%d %v", settlements, err)
	}
	receipt, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, in)
	if err != nil || receipt.AmountMinor != 8000+largeEvidenceSourceCount-1 {
		t.Fatalf("fresh cross-page scope did not recover %+v %v", receipt, err)
	}
	if err := f.h.db.First(&fund, "order_id = ?", pending.ID).Error; err != nil || fund.State != "released" {
		t.Fatalf("fresh scope did not release pending reservation %+v %v", fund, err)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, root.Currency).First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+8000+largeEvidenceSourceCount-1 || wallet.ReservedMinor != 0 {
		t.Fatalf("fresh scope lost reservation/refund value %+v %v", wallet, err)
	}
}
