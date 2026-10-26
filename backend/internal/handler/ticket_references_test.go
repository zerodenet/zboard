package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSQLiteTicketReferencesEnforceOwnerAndCommitWithConversation(t *testing.T) {
	assertTicketReferencesEnforceOwnerAndCommitWithConversation(t, newOrderFixture(t))
}

func TestMySQLTicketReferenceOrderWaitDoesNotHoldOwner(t *testing.T) {
	f := newMySQLBusinessOrderFixture(t)
	order := f.create(t, 0)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseOrder := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseOrder()
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- f.h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var source model.Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, order.ID).Error; err != nil {
				return err
			}
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case err := <-holderDone:
		t.Fatal("source order lock failed", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	const callback = "test:ticket_order_reference_wait"
	entered := make(chan struct{})
	var enteredOnce sync.Once
	if err := f.h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "orders" {
			enteredOnce.Do(func() { close(entered) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.h.db.Callback().Query().Remove(callback) })
	created := make(chan error, 1)
	go func() {
		_, err := f.h.services.Tickets.Create(ctx, 1, experience.NewTicket{TicketNo: "T-REFERENCE-LOCK", Subject: "Pending source order", Category: "billing", Priority: 1, Body: "Check this order.", References: &experience.TicketReferences{OrderID: &order.ID}}, time.Now().UTC())
		created <- err
	}()
	select {
	case <-entered:
	case err := <-created:
		t.Fatal("ticket did not reach source order wait", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// A financial transaction that already owns the source order must still be
	// able to take the owner UPDATE lock. User-first support locks would create
	// the inverse wait and produce a real MySQL NOWAIT conflict here.
	probeErr := f.h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner model.User
		return tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&owner, 1).Error
	})
	releaseOrder()
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	if err := <-created; err != nil {
		t.Fatal("ticket did not recover after source commit", err)
	}
	if probeErr != nil {
		t.Fatal("ticket held owner while waiting for source order", probeErr)
	}
}

func TestMySQLTicketReferencesEnforceOwnerAndCommitWithConversation(t *testing.T) {
	assertTicketReferencesEnforceOwnerAndCommitWithConversation(t, newMySQLBusinessOrderFixture(t))
}

func assertTicketReferencesEnforceOwnerAndCommitWithConversation(t *testing.T, f orderFixture) {
	t.Helper()
	ctx := t.Context()
	priceForFunding(t, f, 100)
	fundTestWallet(t, f, 10000)
	order := f.create(t, 0)
	if _, err := fundingService(f).Configure(ctx, 1, order.ID, commerce.FundingRequest{UseBalance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fundingService(f).Pay(ctx, 1, order.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.First(&order, order.ID).Error; err != nil || order.SubscriptionID == 0 {
		t.Fatal("reference source purchase was not fulfilled", err)
	}
	var entry model.WalletEntry
	if err := f.h.db.Where("user_id = ?", 1).Order("id DESC").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	refs := &experience.TicketReferences{OrderID: &order.ID, SubscriptionID: &order.SubscriptionID, WalletEntryID: &entry.ID}
	create := func(token string, refs *experience.TicketReferences) (*httptest.ResponseRecorder, experience.TicketDetail) {
		t.Helper()
		raw, err := json.Marshal(ticketCreateRequest{Subject: "Account billing context", Category: "billing", Priority: 1, Body: "Please review these owned records.", References: refs})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		f.h.TicketCreateHandler(w, announcementRequest(http.MethodPost, "/api/v1/tickets", token, string(raw)))
		var response struct{ Data experience.TicketDetail }
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
		}
		return w, response.Data
	}
	w, detail := create(f.token, refs)
	if w.Code != http.StatusOK || detail.Ticket.References == nil || len(detail.Messages) != 1 {
		t.Fatalf("owned references not committed: status=%d body=%s", w.Code, w.Body.String())
	}
	assertRefs := func(got *experience.TicketReferences) {
		t.Helper()
		if got == nil || got.OrderID == nil || *got.OrderID != order.ID || got.SubscriptionID == nil || *got.SubscriptionID != order.SubscriptionID || got.WalletEntryID == nil || *got.WalletEntryID != entry.ID {
			t.Fatalf("support context changed: %+v", got)
		}
	}
	assertRefs(detail.Ticket.References)
	other := model.User{Email: "other-ticket-context@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := f.h.issueToken(authClaims{UserID: other.ID, Email: other.Email})
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [2]int64 {
		t.Helper()
		var result [2]int64
		for i, table := range []string{"tickets", "ticket_messages"} {
			if err := f.h.db.Table(table).Count(&result[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	before := counts()
	for _, invalid := range []*experience.TicketReferences{
		{OrderID: &order.ID}, {SubscriptionID: &order.SubscriptionID}, {WalletEntryID: &entry.ID}, refs,
	} {
		if w, _ := create(otherToken, invalid); w.Code != http.StatusBadRequest {
			t.Fatalf("another owner reference accepted: status=%d body=%s", w.Code, w.Body.String())
		}
	}
	missing, zero := uint(99999999), uint(0)
	for _, invalid := range []*experience.TicketReferences{
		{OrderID: &missing}, {SubscriptionID: &missing}, {WalletEntryID: &missing}, {OrderID: &zero},
		{OrderID: &order.ID, SubscriptionID: &missing},
	} {
		if w, _ := create(f.token, invalid); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid context accepted: status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if counts() != before {
		t.Fatal("reference rejection committed a partial conversation")
	}
	// Even an administrator creating through the member API can only attach
	// their own records. Administrator detail access uses the existing scope.
	if err := f.h.db.Model(&other).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := f.h.issueToken(authClaims{UserID: other.ID, Email: other.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := create(adminToken, refs); w.Code != http.StatusBadRequest {
		t.Fatal("member create widened administrator reference ownership", w.Code)
	}
	for _, test := range []struct {
		path string
		want int
	}{
		{fmt.Sprintf("/api/v1/tickets/%d", detail.Ticket.ID), http.StatusForbidden},
		{fmt.Sprintf("/api/v1/admin/tickets/%d", detail.Ticket.ID), http.StatusOK},
	} {
		w := httptest.NewRecorder()
		f.h.TicketGetHandler(w, announcementRequest(http.MethodGet, test.path, adminToken, ""))
		if w.Code != test.want {
			t.Fatalf("ticket detail scope changed: %s status=%d body=%s", test.path, w.Code, w.Body.String())
		}
		if w.Code == http.StatusOK {
			var body struct{ Data experience.TicketDetail }
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			assertRefs(body.Data.Ticket.References)
		}
	}
	w, legacy := create(f.token, nil)
	if w.Code != http.StatusOK || legacy.Ticket.References != nil {
		t.Fatal("unlinked ticket acquired fabricated history", w.Code, legacy.Ticket.References)
	}
	// The initial message and references share the existing ticket transaction.
	// Failure after ticket insert must not leave context or a partial ticket.
	const callback = "test:reject_ticket_context_initial_message"
	if err := f.h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "ticket_messages" {
			tx.AddError(errors.New("injected initial message failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.h.db.Callback().Create().Remove(callback) })
	before = counts()
	if w, _ := create(f.token, refs); w.Code != http.StatusInternalServerError {
		t.Fatalf("initial-message failure was hidden: %d %s", w.Code, w.Body.String())
	}
	if counts() != before {
		t.Fatal("message failure committed ticket or references")
	}
	if err := f.h.db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	walletMatches(t, f, 9900, 0, 3)
	var original model.Order
	if err := f.h.db.First(&original, order.ID).Error; err != nil || original.PaidAmount != order.PaidAmount || original.SubscriptionID != order.SubscriptionID || original.Status != order.Status {
		t.Fatal("support association changed purchase facts", err)
	}
}
