package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type planGroupFinancialState struct {
	Orders        []model.Order
	Subscriptions []model.Subscription
	Accounts      []model.WalletAccount
	Entries       []model.WalletEntry
	Funding       []model.OrderFunding
	Events        []model.PaymentEvent
	Receipts      []model.PaymentReceipt
	Queues        []model.NodeConfigPublish
}

func planGroupFinancialSnapshot(t *testing.T, db *gorm.DB) planGroupFinancialState {
	t.Helper()
	var state planGroupFinancialState
	for _, target := range []any{&state.Orders, &state.Subscriptions, &state.Entries, &state.Events, &state.Receipts} {
		if err := db.Order("id").Find(target).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Order("user_id, currency").Find(&state.Accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("order_id").Find(&state.Funding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("node_id").Find(&state.Queues).Error; err != nil {
		t.Fatal(err)
	}
	return state
}

// An actual endpoint edit retains its endpoint/credential locks while removing
// group membership. A busy payment must keep its financial intent unchanged;
// after that edit commits the same old order uses the group's current scope.
func TestMySQLPlanGroupResourceBusyKeepsPaymentIntentAndRetriesExactlyOnce(t *testing.T) {
	for _, channel := range []string{"wallet", "payment_fact"} {
		t.Run(channel, func(t *testing.T) {
			base := newMySQLOrderFixture(t)
			f := newPlanGroupFixture(t, base)
			*f.at = time.Now().UTC()
			admin := planQuotaRaceAdmin(t, f.fullReplacementFixture)
			pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "busy-unchanged-payment-" + channel})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: channel == "wallet"}); err != nil {
				t.Fatal(err)
			}
			beforeFinancial := planGroupFinancialSnapshot(t, f.db)
			originalOrder := beforeFinancial.Orders[len(beforeFinancial.Orders)-1]
			service := network.ProtocolEndpointMutations{Repository: networkstore.ProtocolEndpointMutations{DB: f.db}, Cipher: base.h.credentialCipher, NewRuntimeKey: uuid.NewString}
			before, err := service.Load(t.Context(), admin, f.old.ID)
			if err != nil {
				t.Fatal(err)
			}
			edit := planGroupEndpointEdit(before.Endpoint)
			edit.Port, edit.PublicPort = 8443, 8443
			var group model.NodeGroup
			if err := f.db.First(&group, base.group.ID).Error; err != nil {
				t.Fatal(err)
			}
			edit.MembershipChanges = []network.ProtocolEndpointMembershipChange{{NodeGroupID: group.ID, ExpectedRevision: group.Revision, Member: false}}
			type mutationMarker struct{}
			held, release := make(chan struct{}), make(chan struct{})
			var entered, unblock sync.Once
			defer unblock.Do(func() { close(release) })
			const hook = "test:membership-edit-retains-endpoint"
			if err := f.db.Callback().Update().After("gorm:update").Register(hook, func(q *gorm.DB) {
				if q.Error == nil && q.RowsAffected > 0 && q.Statement.Context.Value(mutationMarker{}) == true && q.Statement.Table == "protocol_endpoints" {
					entered.Do(func() { close(held); <-release })
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer f.db.Callback().Update().Remove(hook)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			mutationDone := make(chan error, 1)
			go func() {
				_, err := service.Save(context.WithValue(ctx, mutationMarker{}, true), admin, &before, edit)
				mutationDone <- err
			}()
			select {
			case <-held:
			case err := <-mutationDone:
				t.Fatalf("membership edit did not retain endpoint locks: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			fact := commerce.PaymentFact{OrderID: pending.ID, ProviderEventID: "busy-fact-event", ProviderTradeNo: "busy-fact-trade", Status: "paid", AmountMinor: pending.PayableAmount, Currency: "CNY", OccurredAt: *f.at}
			facts := receiptService(base)
			authority := commerce.PaymentFactAuthority{AccountID: 1, Provider: "busy-fact-provider"}
			if channel == "wallet" {
				_, err = f.payments.Pay(ctx, 1, pending.ID)
			} else {
				_, err = facts.Record(ctx, authority, fact)
			}
			if !errors.Is(err, network.ErrResourcesBusy) {
				t.Fatalf("topology contention was not an explicit retryable resource conflict: %v", err)
			}
			if got := planGroupFinancialSnapshot(t, f.db); !reflect.DeepEqual(beforeFinancial, got) {
				t.Fatal("resource conflict committed payment fact, reservation, ledger, entitlement or delivery intent")
			}
			if channel == "wallet" {
				response := httptest.NewRecorder()
				request := announcementRequest("POST", fmt.Sprintf("/api/v1/orders/%d/wallet-pay", pending.ID), base.token, "{}")
				base.h.WalletPayOrderHandler(response, request)
				if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
					t.Fatalf("wallet resource conflict HTTP contract: %d %s headers=%v", response.Code, response.Body.String(), response.Header())
				}
			}
			unblock.Do(func() { close(release) })
			if err := <-mutationDone; err != nil {
				t.Fatalf("membership edit failed after busy payment rolled back: %v", err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if channel == "wallet" {
					_, err = f.payments.Pay(t.Context(), 1, pending.ID)
				} else {
					_, err = facts.Record(t.Context(), authority, fact)
				}
				if err != nil {
					t.Fatalf("unchanged payment retry %d failed: %v", attempt, err)
				}
				if attempt == 0 {
					beforeFinancial = planGroupFinancialSnapshot(t, f.db)
				} else if got := planGroupFinancialSnapshot(t, f.db); !reflect.DeepEqual(beforeFinancial, got) {
					t.Fatal("same payment retry duplicated financial or delivery side effects")
				}
			}
			var paid model.Order
			if err := f.db.First(&paid, pending.ID).Error; err != nil || paid.Status != "paid" || paid.PurchaseSnapshot != originalOrder.PurchaseSnapshot || paid.PayableAmount != originalOrder.PayableAmount {
				t.Fatal("payment retry lost original paid agreement", err)
			}
			var removed int64
			if err := f.db.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND protocol_endpoint_id = ? AND status IN ?", paid.SubscriptionID, f.old.ID, []string{"active", "prepared"}).Count(&removed).Error; err != nil || removed != 0 {
				t.Fatal("pending payment revived removed membership", err)
			}
			var active model.ProtocolCredential
			if err := f.db.Where("subscription_id = ? AND protocol_endpoint_id = ?", paid.SubscriptionID, f.shared.ID).First(&active).Error; err != nil || !active.DeliveryRequired {
				t.Fatal("payment retry did not prepare current shared endpoint", err)
			}
		})
	}
}

func TestMySQLPlanGroupResourceBusyDoesNotAdvanceBufferedCheckpoint(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Updates(map[string]any{"flow_total": 30, "reset_quota_bytes": 30}).Error; err != nil {
		t.Fatal(err)
	}
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, 1)}
	blocker := h.db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var endpoint model.ProtocolEndpoint
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, credential.ProtocolEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if count, err := h.consumeZeroEventBatch(ctx, spool, 32); !errors.Is(err, network.ErrResourcesBusy) || count != 0 || spool.position != 0 {
		t.Fatalf("busy topology advanced received facts/checkpoint: count=%d position=%d error=%v", count, spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	for _, table := range []string{"traffic_records", "flow_usages", "zero_event_node_cursors"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("busy buffer left committed %s=%d: %v", table, count, err)
		}
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if count, err := h.consumeZeroEventBatch(t.Context(), spool, 32); err != nil || count != 1 || spool.position != 1 {
		t.Fatalf("unchanged buffered event retry failed count=%d position=%d: %v", count, spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	spool.position = 0 // Simulate replay after a lost checkpoint acknowledgement.
	if count, err := h.consumeZeroEventBatch(t.Context(), spool, 32); err != nil || count != 1 || spool.position != 1 {
		t.Fatalf("buffer replay failed count=%d position=%d: %v", count, spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	var records int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&records).Error; err != nil || records != 1 {
		t.Fatal("buffer replay repeated charge", err)
	}
}

func TestMySQLPlanGroupResourceBusyOnCredentialNodeForeignKey(t *testing.T) {
	base := newMySQLOrderFixture(t)
	f := newFullReplacementFixture(t, base, false, 0, 0)
	pending, err := f.orders.Create(t.Context(), 1, commerce.OrderCreateRequest{PlanSKUID: base.skuRecord.ID, IdempotencyKey: "busy-node-fk"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.payments.Configure(t.Context(), 1, pending.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	before := planGroupFinancialSnapshot(t, f.db)
	blocker := f.db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer blocker.Rollback()
	var node model.Node
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, f.oldCredential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := f.payments.Pay(ctx, 1, pending.ID); !errors.Is(err, network.ErrResourcesBusy) {
		t.Fatalf("node FK parent lock did not fail immediately: %v", err)
	}
	if got := planGroupFinancialSnapshot(t, f.db); !reflect.DeepEqual(before, got) {
		t.Fatal("node FK conflict partially committed payment/entitlement/publication")
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.payments.Pay(t.Context(), 1, pending.ID); err != nil {
			t.Fatal("unchanged payment failed after node parent unlocked", err)
		}
		if i == 0 {
			before = planGroupFinancialSnapshot(t, f.db)
		} else if got := planGroupFinancialSnapshot(t, f.db); !reflect.DeepEqual(before, got) {
			t.Fatal("node FK retry duplicated money or entitlement")
		}
	}
}
