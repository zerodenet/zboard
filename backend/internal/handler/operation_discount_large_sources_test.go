package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestOperationDiscountLargeSourcesHTTPReplacement(t *testing.T) {
	assertOperationDiscountLargeSources(t, newOrderFixture(t))
}
func TestMySQLOperationDiscountLargeSourcesHTTPReplacement(t *testing.T) {
	assertOperationDiscountLargeSources(t, newMySQLBusinessOrderFixture(t))
}

// Only the quote transport and 1199 prior captured channel facts are fixtures.
// HTTP approval, persisted quote, source budgets, refunds, wallet funding and
// fulfillment are actual core paths; this is not external provider acceptance.
func assertOperationDiscountLargeSources(t *testing.T, base orderFixture) {
	t.Helper()
	f, root, captured := largeCapturedPurchaseFixture(t, base)
	plan := f.plan(t, 2)
	if err := f.h.db.Model(&plan).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	sku := f.sku(t, plan.ID, 2000, "change")
	var originalSub model.Subscription
	var originalIntents []model.NodeConfigPublish
	if err := f.h.db.First(&originalSub, root.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Order("node_id").Find(&originalIntents).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	quoter := operationCheckoutQuoter(func(ctx context.Context, provider string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
		if err := ctx.Err(); err != nil {
			return commerce.DiscountQuote{}, err
		}
		calls++
		body, err := json.Marshal(in)
		if err != nil || len(body) > 2048 || strings.Contains(string(body), "Sources") || strings.Contains(string(body), "sources") || in.Operation != "upgrade" || in.TargetSubscriptionID != root.SubscriptionID || in.BasePriceMinor != 2000 {
			t.Fatal("provider received wrong operation or private source history", in, len(body), err)
		}
		terms := `{"discount_minor":200,"scope":"order_only"}`
		hash, _ := commerce.DiscountTermsHash(terms)
		return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: provider, SigningKeyFingerprint: strings.Repeat("a", 64), PackageDigest: strings.Repeat("b", 64), Generation: 1, ConfigRevision: 1}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 200, ExpiresAt: time.Now().UTC().Add(time.Hour), Agreement: &commerce.DiscountAgreement{ID: "large-sources-one-order", Version: "1", Scope: commerce.OperationDiscountScope, TermsJSON: terms, TermsHash: hash}}}, nil
	})
	f.h.services.OrderCreation = commerce.OrderCreation{Repository: commercestore.OrderCreation{DB: f.h.db, Discounts: quoter}}
	request := commerce.OrderCreateRequest{PlanSKUID: sku.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "large-sources-one-order-change", Discount: &commerce.OrderDiscountSelection{ProviderID: "example.large-one-order", Selection: "once"}}
	call := func(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		out := httptest.NewRecorder()
		handler(out, announcementRequest(http.MethodPost, path, f.token, string(raw)))
		return out
	}
	batches, remove := guardLargeEvidencePages(t, f.h.db)
	previewResponse := call(f.h.OrderPreviewHandler, "/api/v1/orders/preview")
	var preview struct{ Data commerce.OrderPreview }
	want := int64(8000 + largeEvidenceSourceCount - 1)
	if previewResponse.Code != http.StatusOK || json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil || preview.Data.BasePriceMinor != 2000 || preview.Data.DiscountAmount != 200 || preview.Data.PayableAmount != 1800 || preview.Data.SettlementAmount != want || preview.Data.CreditAmount != 1800 || preview.Data.SurplusAmount != want-1800 || preview.Data.AdditionalAmount != 0 || preview.Data.Discount == nil || preview.Data.Discount.Offer.Agreement == nil || preview.Data.Discount.Offer.Agreement.Scope != commerce.OperationDiscountScope {
		t.Fatal("1200-source order-only HTTP quote", previewResponse.Code, previewResponse.Body.String())
	}
	if response := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders"); response.Code != http.StatusBadRequest {
		t.Fatal("unconfirmed large-source offer accepted", response.Code, response.Body.String())
	}
	request.QuoteFingerprint = preview.Data.QuoteFingerprint
	createdResponse := call(f.h.OrderCreateCommerceValidatedHandler, "/api/v1/orders")
	var created struct{ Data commerce.Order }
	if createdResponse.Code != http.StatusOK || json.Unmarshal(createdResponse.Body.Bytes(), &created) != nil || created.Data.ID == 0 {
		t.Fatal("1200-source order-only HTTP create", createdResponse.Code, createdResponse.Body.String())
	}
	var stored model.Order
	if err := f.h.db.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	policy, err := commerce.DecodePurchaseSnapshot(stored.PurchaseSnapshot)
	var change commerce.SubscriptionChangeSnapshot
	if err != nil || policy.Discount == nil || policy.Discount.Version != 3 || commerce.ValidatePurchaseDiscount(commerce.Order(stored), policy) != nil || json.Unmarshal([]byte(stored.ChangeSnapshot), &change) != nil || len(change.Sources) != largeEvidenceSourceCount || change.Residual != want || change.Applied != 1800 || change.Surplus != want-1800 {
		t.Fatal("large-source acceptance lost monetary/source binding", err)
	}
	if _, err := fundingService(f).Configure(t.Context(), 1, stored.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	var configuredOrder model.Order
	var configuredFunding model.OrderFunding
	if err := f.h.db.First(&configuredOrder, stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Where("order_id = ?", stored.ID).First(&configuredFunding).Error; err != nil {
		t.Fatal(err)
	}
	// Fail after all source refunds and the gross wallet credit have executed.
	// The same owning transaction must roll back every source and reservation.
	const hook = "test:large-discount-settlement-sql-failure"
	var injected atomic.Bool
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "subscription.change.settle" {
			injected.Store(true)
			tx.AddError(tx.Exec("INSERT INTO zboard_test_missing_large_discount_table(id) VALUES(1)").Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Create().Remove(hook) })
	if _, err := fundingService(f).Pay(t.Context(), 1, stored.ID); err == nil || !injected.Load() || !strings.Contains(err.Error(), "zboard_test_missing_large_discount_table") {
		t.Fatal("late financial SQL failure did not abort", err)
	}
	var rollbackSub model.Subscription
	var rollbackWallet model.WalletAccount
	var rollbackIntents []model.NodeConfigPublish
	if err := f.h.db.First(&rollbackSub, originalSub.ID).Error; err != nil || !reflect.DeepEqual(rollbackSub, originalSub) {
		t.Fatal("failed discount payment changed rights", rollbackSub, err)
	}
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&rollbackWallet).Error; err != nil || rollbackWallet.BalanceMinor != 20000 || rollbackWallet.ReservedMinor != 0 {
		t.Fatal("failed discount payment changed money", rollbackWallet, err)
	}
	if err := f.h.db.Order("node_id").Find(&rollbackIntents).Error; err != nil || !reflect.DeepEqual(rollbackIntents, originalIntents) {
		t.Fatal("failed discount payment committed publications", err)
	}
	for _, table := range []any{&model.OrderValueBudget{}, &model.SubscriptionValueSettlement{}, &model.SubscriptionValueSettlementSource{}} {
		var count int64
		if err := f.h.db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed discount payment retained financial authority %T count=%d err=%v", table, count, err)
		}
	}
	var rollbackOrder model.Order
	var rollbackFunding model.OrderFunding
	if err := f.h.db.First(&rollbackOrder, stored.ID).Error; err != nil || !reflect.DeepEqual(rollbackOrder, configuredOrder) {
		t.Fatal("failed discount payment changed pending order", err)
	}
	if err := f.h.db.Where("order_id = ?", stored.ID).First(&rollbackFunding).Error; err != nil || !reflect.DeepEqual(rollbackFunding, configuredFunding) {
		t.Fatal("failed discount payment captured or changed funding", err)
	}
	var refunded int64
	if err := f.h.db.Model(&model.Order{}).Where("subscription_id = ?", root.SubscriptionID).Select("COALESCE(SUM(refund_amount),0)").Scan(&refunded).Error; err != nil || refunded != 0 {
		t.Fatal("failed discount payment retained source refunds", refunded, err)
	}
	if err := f.h.db.Callback().Create().Remove(hook); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := fundingService(f).Pay(t.Context(), 1, stored.ID); err != nil {
			t.Fatal("large-source discounted fulfillment/replay", err)
		}
	}
	remove()
	if batches.Load() < 6 || calls == 0 {
		t.Fatal("large-source operation did not traverse bounded evidence/quote path", batches.Load(), calls)
	}
	var wallet model.WalletAccount
	if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want-1800 || wallet.ReservedMinor != 0 {
		t.Fatal("discount or source replay minted/lost money", wallet, err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.PlanID != plan.ID || sub.FlowUsed != 20 || sub.EntitlementEpoch != 1 {
		t.Fatal("replacement changed subscription identity/old use", sub, err)
	}
	if total, used := entitlements.CycleQuota(entitlements.Subscription(sub)); total != 100 || used != 0 {
		t.Fatal("replacement stacked already converted rights", total, used)
	}
	var paid model.Order
	if err := f.h.db.First(&paid, stored.ID).Error; err != nil || paid.Status != "paid" || paid.PaidAmount != 1800 || paid.AmountCents != 2000 || paid.DiscountAmount != 200 {
		t.Fatal("replacement did not retain actual discounted payment", err)
	}
	originals := map[uint]model.Order{root.ID: root}
	for _, row := range captured {
		originals[row.ID] = row
	}
	var old []model.Order
	if err := f.h.db.Where("subscription_id = ? AND id <> ?", root.SubscriptionID, stored.ID).Order("id ASC").Find(&old).Error; err != nil || len(old) != largeEvidenceSourceCount {
		t.Fatal("replacement lost source history", len(old), err)
	}
	for _, row := range old {
		original, ok := originals[row.ID]
		wantRefund := int64(1)
		if row.ID == root.ID {
			wantRefund = 8000
		}
		if !ok || row.PaidAmount != original.PaidAmount || row.EntitlementValueSnapshot != original.EntitlementValueSnapshot || row.PurchaseSnapshot != original.PurchaseSnapshot || row.RefundAmount != wantRefund || row.RefundAmount > row.PaidAmount {
			t.Fatal("source payment/immutable proof/cumulative refund cap changed", row.ID)
		}
	}
	var settlements int64
	var sourceCount, sourceTotal int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Where("subscription_id = ? AND kind = ?", sub.ID, "change").Count(&settlements).Error; err != nil || settlements != 1 {
		t.Fatal("discounted replacement settled more than once", settlements, err)
	}
	if err := f.h.db.Model(&model.SubscriptionValueSettlementSource{}).Count(&sourceCount).Error; err != nil || sourceCount != largeEvidenceSourceCount {
		t.Fatal("source settlement journal incomplete", sourceCount, err)
	}
	if err := f.h.db.Model(&model.SubscriptionValueSettlementSource{}).Select("COALESCE(SUM(amount_minor),0)").Scan(&sourceTotal).Error; err != nil || sourceTotal != want {
		t.Fatal("source settlement sum changed", sourceTotal, err)
	}
	var audits int64
	if err := f.h.db.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "subscription.change.settle", fmt.Sprintf("order:%d", stored.ID)).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("discount settlement audit absent or duplicated", audits, err)
	}
}
