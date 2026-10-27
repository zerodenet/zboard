package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestWholeTerminationPreservesLargeClosedHistory(t *testing.T) {
	assertTerminationHistory(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}
func TestMySQLWholeTerminationPreservesLargeClosedHistory(t *testing.T) {
	assertTerminationHistory(t, newMySQLBusinessOrderFixture)
}

// The old attempts are deliberately not reconstructed as trusted purchases.
// Their closed term is irrelevant to current value, and their original rows
// must survive a current refund or an explicit zero-money whole termination.
func seedClosedTerminationHistory(t *testing.T, f orderFixture, root model.Order, start time.Time) [32]byte {
	t.Helper()
	ended := start.Add(-24 * time.Hour)
	rows := make([]model.Order, 1200)
	for i := range rows {
		status := []string{"paid", "failed", "canceled"}[i%3]
		rows[i] = model.Order{UserID: root.UserID, SubscriptionID: root.SubscriptionID, PlanID: root.PlanID, PlanSKUID: root.PlanSKUID,
			TradeNo: fmt.Sprintf("closed-history-%d", i), Status: status, OrderType: "renewal", Currency: "CNY",
			AmountCents: 1234, PayableAmount: 1000, PaidAmount: 0, PurchaseSnapshot: "old evidence retained verbatim",
			SubscriptionEndedAt: &ended, SubscriptionEndReason: "changed", SubscriptionFinalFlowTotal: 57, SubscriptionFinalFlowUsed: 43,
			CreatedAt: ended.Add(-time.Hour), UpdatedAt: ended}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	return closedTerminationHistoryDigest(t, f)
}

func closedTerminationHistoryDigest(t *testing.T, f orderFixture) [32]byte {
	t.Helper()
	var rows []model.Order
	if err := f.h.db.Where("trade_no LIKE ?", "closed-history-%").Order("id").Find(&rows).Error; err != nil || len(rows) != 1200 {
		t.Fatalf("closed history rows=%d err=%v", len(rows), err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

func assertTerminationHistory(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, mode := range []string{"owner_refund", "admin_unknown_zero", "legacy_zero"} {
		t.Run(mode, func(t *testing.T) {
			f, root := terminationHistoryFixture(t, fixture(t))
			var sub model.Subscription
			if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil {
				t.Fatal(err)
			}
			// Reproduce an interrupted old cancellation with a surviving hold.
			// It must be selected even though its end predates the current term.
			held := f.create(t, sub.ID)
			if _, err := fundingService(f).Configure(t.Context(), 1, held.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
				t.Fatal(err)
			}
			ended := sub.StartAt.Add(-time.Hour)
			if err := f.h.db.Model(&model.Order{}).Where("id = ?", held.ID).Updates(map[string]any{"status": "failed", "subscription_ended_at": ended, "subscription_end_reason": "changed"}).Error; err != nil {
				t.Fatal(err)
			}
			before := seedClosedTerminationHistory(t, f, root, sub.StartAt)
			if mode == "admin_unknown_zero" {
				if err := f.h.db.Model(&model.Order{}).Where("id = ?", root.ID).Update("entitlement_value_snapshot", "").Error; err != nil {
					t.Fatal(err)
				}
			}
			var receipt uint
			want := int64(0)
			switch mode {
			case "owner_refund":
				q, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, sub.ID)
				if err != nil || q.TotalMinor != 8000 {
					t.Fatalf("current paid value %+v %v", q, err)
				}
				in := commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "large-closed-owner"}
				for range 2 {
					r, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, in)
					if err != nil || r.AmountMinor != 8000 || receipt != 0 && receipt != r.ID {
						t.Fatalf("owner termination %+v %v", r, err)
					}
					receipt = r.ID
				}
				want = 8000
			case "admin_unknown_zero":
				q, err := f.h.services.SubscriptionValues.ReadForAdministrator(t.Context(), 1, sub.ID)
				if err != nil || q.ValuationStatus != "unavailable" || q.TotalMinor != nil {
					t.Fatalf("unknown default fabricated %+v %v", q, err)
				}
				zero := int64(0)
				in := commerce.AdministrativeSubscriptionTerminationRequest{SubscriptionTerminationRequest: commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "large-closed-zero"}, AmountMinor: &zero, Reason: "operator terminates all remaining rights without refund"}
				for range 2 {
					r, err := f.h.services.SubscriptionTermination.TerminateForAdministrator(t.Context(), 1, sub.ID, in)
					if err != nil || r.AmountMinor != 0 || r.WalletEntryID != nil || receipt != 0 && receipt != r.ID || r.RecordKind != "entitlement_mutation" {
						t.Fatalf("zero termination %+v %v", r, err)
					}
					receipt = r.ID
				}
			case "legacy_zero":
				in := entitlements.SubscriptionMutationInput{SubscriptionID: sub.ID, Kind: entitlements.SubscriptionCancel, Reason: "stop all remaining rights without refund", IdempotencyKey: "large-closed-legacy", Origin: "admin"}
				for range 2 {
					r, err := f.h.services.SubscriptionMutations().Apply(t.Context(), 1, in)
					if err != nil || r.Status != "canceled" || receipt != 0 && receipt != r.MutationID {
						t.Fatalf("legacy termination %+v %v", r, err)
					}
					receipt = r.MutationID
				}
			}
			if receipt == 0 {
				t.Fatal("missing durable termination identity")
			}
			var wallet model.WalletAccount
			if err := f.h.db.Where("user_id = ? AND currency = ?", 1, "CNY").First(&wallet).Error; err != nil || wallet.BalanceMinor != 20000+want || wallet.ReservedMinor != 0 {
				t.Fatalf("whole termination money %+v %v", wallet, err)
			}
			var fund model.OrderFunding
			if err := f.h.db.First(&fund, "order_id = ?", held.ID).Error; err != nil || fund.State != "released" {
				t.Fatalf("historical hold retained %+v %v", fund, err)
			}
			if err := f.h.db.First(&held, held.ID).Error; err != nil || held.Status != "failed" || held.SubscriptionEndedAt == nil || !held.SubscriptionEndedAt.Equal(ended) || held.SubscriptionEndReason != "changed" {
				t.Fatalf("historical end was rewritten %+v %v", held, err)
			}
			if err := f.h.db.First(&sub, sub.ID).Error; err != nil || sub.Status != "canceled" || sub.EndedAt == nil {
				t.Fatalf("remaining rights survived %+v %v", sub, err)
			}
			if before != closedTerminationHistoryDigest(t, f) {
				t.Fatal("closed historical money/evidence/end metadata changed")
			}
			var entries int64
			if err := f.h.db.Model(&model.WalletEntry{}).Where("kind = ?", "subscription_refund").Count(&entries).Error; err != nil || want == 0 && entries != 0 || want > 0 && entries != 1 {
				t.Fatalf("duplicate or zero-money refund count=%d %v", entries, err)
			}
		})
	}
}

func TestWholeTerminationRechecksPreparedOrderMembership(t *testing.T) {
	assertTerminationScopeChange(t, newOrderFixture(t))
}
func TestMySQLWholeTerminationRechecksPreparedOrderMembership(t *testing.T) {
	assertTerminationScopeChange(t, newMySQLBusinessOrderFixture(t))
}
func assertTerminationScopeChange(t *testing.T, f orderFixture) {
	t.Helper()
	f, root := terminationHistoryFixture(t, f)
	q, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, resume := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(resume)
		}
	}()
	var paused atomic.Bool
	if err := f.h.db.Callback().Query().After("gorm:query").Register("test:termination_scope_prepared", func(tx *gorm.DB) {
		if tx.Statement.Table != "orders" {
			return
		}
		if !strings.Contains(strings.Join(tx.Statement.Selects, ","), "AS evidence_bytes") || !paused.CompareAndSwap(false, true) {
			return
		}
		close(prepared)
		<-resume
	}); err != nil {
		t.Fatal(err)
	}
	defer f.h.db.Callback().Query().Remove("test:termination_scope_prepared")
	done := make(chan error, 1)
	go func() {
		_, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "prepared-membership-change"})
		done <- err
	}()
	select {
	case <-prepared:
	case err := <-done:
		t.Fatalf("scope did not reach its unlocked preparation: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("scope preparation did not complete")
	}
	pending := f.create(t, root.SubscriptionID)
	if _, err := fundingService(f).Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		close(resume)
		released = true
		t.Fatal(err)
	}
	close(resume)
	released = true
	if err := <-done; !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
		t.Fatalf("new unlocked reservation was missed: %v", err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
		t.Fatalf("partial rights on scope retry %+v %v", sub, err)
	}
	var fund model.OrderFunding
	if err := f.h.db.First(&fund, "order_id = ?", pending.ID).Error; err != nil || fund.State != "reserved" {
		t.Fatalf("scope retry changed reservation %+v %v", fund, err)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements != 0 {
		t.Fatalf("scope retry committed refund %d %v", settlements, err)
	}
	r, err := f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, sub.ID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "prepared-membership-change"})
	if err != nil || r.AmountMinor != 8000 {
		t.Fatalf("retry after fresh membership %+v %v", r, err)
	}
}

func TestWholeTerminationKeepsCurrentSourceBudget(t *testing.T) {
	assertTerminationSourceBudget(t, newOrderFixture(t))
}
func TestMySQLWholeTerminationKeepsCurrentSourceBudget(t *testing.T) {
	assertTerminationSourceBudget(t, newMySQLBusinessOrderFixture(t))
}
func assertTerminationSourceBudget(t *testing.T, f orderFixture) {
	t.Helper()
	f, root := terminationHistoryFixture(t, f)
	q, err := f.h.services.SubscriptionValues.Read(t.Context(), 1, root.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]model.Order, 1001)
	for i := range rows {
		target := root.SubscriptionID
		rows[i] = model.Order{UserID: 1, TargetSubscriptionID: &target, PlanID: root.PlanID, PlanSKUID: root.PlanSKUID,
			TradeNo: fmt.Sprintf("current-budget-%d", i), Status: "pending", OrderType: "renewal", Currency: "CNY", AmountCents: 100, PayableAmount: 100}
	}
	if err := f.h.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	_, err = f.h.services.SubscriptionTermination.Terminate(t.Context(), 1, root.SubscriptionID, commerce.SubscriptionTerminationRequest{QuoteFingerprint: q.Fingerprint, IdempotencyKey: "current-source-budget"})
	if !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) {
		t.Fatalf("unbounded authority transaction allowed: %v", err)
	}
	var sub model.Subscription
	if err := f.h.db.First(&sub, root.SubscriptionID).Error; err != nil || sub.Status != "active" || sub.EndedAt != nil {
		t.Fatalf("over-budget partially terminated %+v %v", sub, err)
	}
	var settlements int64
	if err := f.h.db.Model(&model.SubscriptionValueSettlement{}).Count(&settlements).Error; err != nil || settlements != 0 {
		t.Fatalf("over-budget refund %d %v", settlements, err)
	}
}

// Full-price actual wallet purchase avoids inventing a recurring agreement for
// the administrator's unrelated manual price override in the other value tests.
func terminationHistoryFixture(t *testing.T, f orderFixture) (orderFixture, model.Order) {
	t.Helper()
	attachOrderPublishEndpoint(t, f)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100, "reset_policy": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&f.skuRecord).Updates(map[string]any{"price_cents": 10000, "billing_unit": "once", "billing_value": 1, "billing_mode": "one_time", "renewal_effect": "add_quota_only"}).Error; err != nil {
		t.Fatal(err)
	}
	fundTestWallet(t, f, 30000)
	root := f.create(t, 0)
	if _, err := fundingService(f).Configure(t.Context(), 1, root.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(t.Context(), 1, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&root, root.ID).Error; err != nil || root.PaidAmount != 10000 {
		t.Fatalf("actual full-price source %+v %v", root, err)
	}
	completePerpetualFIFOUsage(t, f, root.SubscriptionID, "termination-history-actual-usage", 20)
	return f, root
}
