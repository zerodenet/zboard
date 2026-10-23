package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This needs real InnoDB RR and performance_schema: SQLite cannot establish
// snapshot freshness or demonstrate the absence of an opposite owner lock.
func TestMySQLRecurringDiscountPreviewWaitsBeforeOwnerAndRejectsStaleQuote(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	recurringNativeSetup(t, f)
	f.token, _, _ = f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
	var isolation string
	if err := f.h.db.Raw("SELECT @@transaction_isolation").Scan(&isolation).Error; err != nil || !strings.EqualFold(isolation, "REPEATABLE-READ") {
		t.Fatalf("requires real InnoDB RR: isolation=%q error=%v", isolation, err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, IdempotencyKey: "rr-recurring-new", Discount: &commerce.OrderDiscountSelection{ProviderID: "example.quote", Selection: "recurring"}}
	request.QuoteFingerprint = recurringPreview(t, f, request).QuoteFingerprint
	root, _ := recurringWalletPay(t, f, recurringCreate(t, f, request).ID)
	renewalRequest := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "rr-recurring-paid-renewal"}
	renewalRequest.QuoteFingerprint = recurringPreview(t, f, renewalRequest).QuoteFingerprint
	renewal := recurringCreate(t, f, renewalRequest)
	if _, err := fundingService(f).Configure(t.Context(), 1, renewal.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	// Save a different intent before payment changes the paid source and term.
	stale := commerce.OrderCreateRequest{PlanSKUID: f.skuRecord.ID, TargetSubscriptionID: root.SubscriptionID, IdempotencyKey: "rr-recurring-stale"}
	stale.QuoteFingerprint = recurringPreview(t, f, stale).QuoteFingerprint
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type paymentContextKey struct{}
	locked, release := make(chan struct{}), make(chan struct{})
	var reached atomic.Bool
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	const callback = "test:recurring-payment-source-locks"
	if err := f.h.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if tx.Error != nil || tx.Statement.Context.Value(paymentContextKey{}) != true || tx.Statement.Table != "orders" || !ok || locking.Strength != "UPDATE" || !strings.Contains(tx.Statement.SQL.String(), "target_subscription_id") || !reached.CompareAndSwap(false, true) {
			return
		}
		// Actual WalletPayments.Pay has just acquired its canonical related
		// source-order locks; its next step acquires the owner. No SQL mutation
		// or synthetic snapshot is substituted for the production payment.
		close(locked)
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.db.Callback().Query().Remove(callback) })
	type paymentOutcome struct {
		result commerce.WalletPaymentResult
		err    error
	}
	paid := make(chan paymentOutcome, 1)
	go func() {
		result, err := fundingService(f).Pay(context.WithValue(ctx, paymentContextKey{}, true), 1, renewal.ID)
		paid <- paymentOutcome{result, err}
	}()
	paymentJoined := false
	defer func() {
		unblock()
		cancel()
		if !paymentJoined {
			<-paid
		}
	}()
	select {
	case <-locked:
	case result := <-paid:
		paymentJoined = true
		t.Fatalf("payment missed canonical source lock hook: %v", result.err)
	case <-ctx.Done():
		t.Fatal("payment did not acquire canonical source locks", ctx.Err())
	}
	previewDone := make(chan error, 1)
	go func() {
		_, err := f.h.services.OrderCreation.Preview(ctx, 1, stale)
		previewDone <- err
	}()
	previewJoined := false
	defer func() {
		unblock()
		cancel()
		if !previewJoined {
			<-previewDone
		}
	}()
	var schema string
	if err := f.h.db.Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waits int64
		if err := f.h.db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='orders'`, schema).Scan(&waits).Error; err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preview did not wait for actual payment's source-order locks")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// While preview waits on the sources, a separate NOWAIT owner probe must
	// succeed. Otherwise it could deadlock payment's source -> owner sequence.
	probe := f.h.db.WithContext(ctx).Begin()
	if probe.Error != nil {
		t.Fatal(probe.Error)
	}
	var owner model.User
	probeErr := probe.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).Select("id").First(&owner, 1).Error
	rollbackErr := probe.Rollback().Error
	if probeErr != nil || rollbackErr != nil {
		t.Fatalf("preview waiting on sources retained an opposite owner lock: probe=%v rollback=%v", probeErr, rollbackErr)
	}
	unblock()
	result := <-paid
	paymentJoined = true
	if result.err != nil || !result.result.Fulfilled {
		t.Fatalf("actual renewal payment failed: %+v %v", result.result, result.err)
	}
	previewErr := <-previewDone
	previewJoined = true
	var validation *commerce.ValidationError
	if !errors.Is(previewErr, commerce.ErrSubscriptionQuoteChanged) && !errors.As(previewErr, &validation) {
		t.Fatalf("post-wait preview accepted an unprepared paid source or returned unrelated failure: %v", previewErr)
	}
	var beforeOrders, afterOrders int64
	if err := f.h.db.Model(&model.Order{}).Count(&beforeOrders).Error; err != nil {
		t.Fatal(err)
	}
	response := recurringHTTP(t, f, false, stale)
	if (response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "subscription_quote_changed")) && (response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation_failed")) {
		t.Fatalf("stale pre-payment quote was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	if err := f.h.db.Model(&model.Order{}).Count(&afterOrders).Error; err != nil || beforeOrders != afterOrders {
		t.Fatalf("stale quote created an order: before=%d after=%d error=%v", beforeOrders, afterOrders, err)
	}
	walletMatches(t, f, 30000, 0, 5)
}
