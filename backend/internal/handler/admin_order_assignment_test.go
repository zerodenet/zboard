package handler

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func assignmentBuyer(t *testing.T, f orderFixture) model.User {
	t.Helper()
	user := model.User{Email: "assigned@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	return user
}
func assignOrder(t *testing.T, f orderFixture, req adminOrderAssignmentRequest) (*httptest.ResponseRecorder, model.Order) {
	t.Helper()
	payload, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	f.h.AdminOrderAssignHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/orders", f.token, string(payload)))
	var response struct{ Data model.Order }
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	return w, response.Data
}
func assignmentRequest(f orderFixture, id uint) adminOrderAssignmentRequest {
	return adminOrderAssignmentRequest{UserID: id, PlanSKUID: f.skuRecord.ID, Note: "客户补偿", RequestID: uuid.NewString()}
}
func TestAdminAssignmentCreatesPendingForBuyerAndRetriesOnce(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	req := assignmentRequest(f, buyer.ID)
	w, order := assignOrder(t, f, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if order.UserID != buyer.ID || order.SubscriptionID != 0 || order.Status != orderStatusPending || order.PaidAmount != 0 || order.PayableAmount != f.skuRecord.PriceCents || order.DiscountAmount != 0 {
		t.Fatalf("order: %+v", order)
	}
	var stored model.Order
	f.h.db.First(&stored, order.ID)
	if stored.AssignedBy != 1 || stored.AssignmentNote != req.Note {
		t.Fatalf("provenance: %+v", stored)
	}
	w, replay := assignOrder(t, f, req)
	if w.Code != 200 || replay.ID != order.ID {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	if w := f.pay(t, order.ID, false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := f.pay(t, order.ID, false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, table := range []string{"orders", "subscriptions", "quota_events"} {
		var count int64
		f.h.db.Table(table).Count(&count)
		if count != 1 {
			t.Fatalf("%s count=%d", table, count)
		}
	}
	var count int64
	f.h.db.Model(&model.AuditLog{}).Where("action = ?", "order.assign").Count(&count)
	if count != 1 {
		t.Fatalf("audit count=%d", count)
	}
	req.Note = "changed request"
	if w, _ := assignOrder(t, f, req); w.Code != 400 {
		t.Fatalf("changed retry=%d", w.Code)
	}
}
func TestAdminAssignmentPendingDefersEntitlement(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	w, order := assignOrder(t, f, assignmentRequest(f, buyer.ID))
	if w.Code != 200 || order.UserID != buyer.ID || order.Status != orderStatusPending || order.SubscriptionID != 0 || order.PayableAmount != f.skuRecord.PriceCents || order.PaidAmount != 0 {
		t.Fatalf("pending: %d %s", w.Code, w.Body.String())
	}
	var count int64
	f.h.db.Model(&model.Subscription{}).Count(&count)
	if count != 0 {
		t.Fatal("premature entitlement")
	}
	paid := f.paid(t, order.ID)
	if paid.UserID != buyer.ID || paid.PaidAmount != f.skuRecord.PriceCents || paid.SubscriptionID == 0 {
		t.Fatalf("paid: %+v", paid)
	}
}
func TestAdminAssignmentValidationAndAuthorization(t *testing.T) {
	for _, scenario := range []string{"non-admin", "missing-user", "disabled-user", "inactive-sku", "foreign-subscription", "missing-note", "missing-request"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOrderFixture(t)
			buyer := assignmentBuyer(t, f)
			req := assignmentRequest(f, buyer.ID)
			expected := 400
			switch scenario {
			case "non-admin":
				f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false)
				token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test"})
				if err != nil {
					t.Fatal(err)
				}
				f.token = token
				expected = 403
			case "missing-user":
				req.UserID = 99999
			case "disabled-user":
				f.h.db.Model(&buyer).Update("status", "disabled")
			case "inactive-sku":
				f.h.db.Model(&f.skuRecord).Update("is_active", false)
			case "foreign-subscription":
				req.TargetSubscriptionID = f.paid(t, f.create(t, 0).ID).SubscriptionID
			case "missing-note":
				req.Note = " "
			case "missing-request":
				req.RequestID = ""
			}
			var before int64
			f.h.db.Model(&model.Order{}).Count(&before)
			w, _ := assignOrder(t, f, req)
			if w.Code != expected {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var after int64
			f.h.db.Model(&model.Order{}).Count(&after)
			if before != after {
				t.Fatal("invalid order persisted")
			}
		})
	}
}
func TestAdminAssignmentRollbackAndCapacity(t *testing.T) {
	for _, scenario := range []string{"capacity", "audit"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOrderFixture(t)
			buyer := assignmentBuyer(t, f)
			expected := 500
			if scenario == "capacity" {
				f.h.db.Model(&f.planRecord).Update("max_active_subscriptions", 1)
				f.paid(t, f.create(t, 0).ID)
				expected = 409
			}
			if scenario == "audit" {
				if err := f.h.db.Exec(`CREATE TRIGGER reject_assignment_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			w, _ := assignOrder(t, f, assignmentRequest(f, buyer.ID))
			if w.Code != expected {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			for _, table := range []string{"orders", "subscriptions"} {
				var count int64
				f.h.db.Table(table).Where("user_id = ?", buyer.ID).Count(&count)
				if count != 0 {
					t.Fatalf("partial %s", table)
				}
			}
		})
	}
}
func TestAdminAssignmentExplicitRenewal(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	w, first := assignOrder(t, f, assignmentRequest(f, buyer.ID))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	first = f.paid(t, first.ID)
	var before model.Subscription
	f.h.db.First(&before, first.SubscriptionID)
	req := assignmentRequest(f, buyer.ID)
	req.TargetSubscriptionID = first.SubscriptionID
	w, renewal := assignOrder(t, f, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	renewal = f.paid(t, renewal.ID)
	var after model.Subscription
	f.h.db.First(&after, first.SubscriptionID)
	if renewal.OrderType != "renewal" || renewal.SubscriptionID != first.SubscriptionID || !after.EndAt.After(before.EndAt) || after.FlowTotal != before.FlowTotal {
		t.Fatalf("renewal: %+v %+v", renewal, after)
	}
}

func TestAdminAssignmentCustomPaymentAmount(t *testing.T) {
	for _, amount := range []int64{0, 25, 150} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			f := newOrderFixture(t)
			buyer := assignmentBuyer(t, f)
			req := assignmentRequest(f, buyer.ID)
			req.PayableAmount = &amount
			w, order := assignOrder(t, f, req)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if order.Status != orderStatusPending || order.AmountCents != 100 || order.PayableAmount != amount || order.DiscountAmount != max(100-amount, 0) || order.PaidAmount != 0 {
				t.Fatalf("amount snapshot: %+v", order)
			}
			// Recipient sees the overridden amount; another user's list does not expose it.
			token, _, err := f.h.issueToken(authClaims{UserID: buyer.ID, Email: buyer.Email})
			if err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			f.h.OrderListHandler(w, announcementRequest("GET", "/api/v1/orders", token, ""))
			var list struct{ Data []adminOrderListItem }
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Data) != 1 || list.Data[0].PayableAmount != amount {
				t.Fatalf("buyer list: %s", w.Body.String())
			}
			w = httptest.NewRecorder()
			f.h.OrderListHandler(w, announcementRequest("GET", "/api/v1/orders", f.token, ""))
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Data) != 0 {
				t.Fatalf("foreign order leaked: %s", w.Body.String())
			}
			paid := f.paid(t, order.ID)
			aggregate, err := f.h.dashboardOrderAggregate(time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
			if err != nil || aggregate.RevenueCents != amount {
				t.Fatalf("custom amount revenue: %+v %v", aggregate, err)
			}
			if paid.PaidAmount != amount || paid.SubscriptionID == 0 {
				t.Fatalf("paid wrong amount: %+v", paid)
			}
			var sub model.Subscription
			f.h.db.First(&sub, paid.SubscriptionID)
			if sub.UserID != buyer.ID || sub.FlowTotal != 1024 {
				t.Fatalf("buyer entitlement: %+v", sub)
			}
			w, replay := assignOrder(t, f, req)
			if w.Code != 200 || replay.ID != order.ID || replay.SubscriptionID != paid.SubscriptionID {
				t.Fatalf("paid replay: %d %s", w.Code, w.Body.String())
			}
			changed := amount + 1
			req.PayableAmount = &changed
			w, _ = assignOrder(t, f, req)
			if w.Code != 400 {
				t.Fatal("changed payment amount reused request ID")
			}
		})
	}
}
func TestAdminAssignmentRejectsInvalidPaymentAmount(t *testing.T) {
	for _, amount := range []int64{-1, 9007199254740992} {
		f := newOrderFixture(t)
		buyer := assignmentBuyer(t, f)
		req := assignmentRequest(f, buyer.ID)
		req.PayableAmount = &amount
		w, _ := assignOrder(t, f, req)
		if w.Code != 400 {
			t.Fatalf("invalid amount: %d %s", w.Code, w.Body.String())
		}
		var count int64
		f.h.db.Model(&model.Order{}).Count(&count)
		if count != 0 {
			t.Fatal("invalid amount persisted")
		}
	}
}

func TestSelfServiceOrderCannotOverrideBuyerOrPaymentAmount(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	token, _, err := f.h.issueToken(authClaims{UserID: buyer.ID, Email: buyer.Email})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.OrderCreateCommerceValidatedHandler(w, announcementRequest("POST", "/api/v1/orders", token, fmt.Sprintf(`{"plan_sku_id":%d,"user_id":1,"payable_amount":1,"assigned_by":1}`, f.skuRecord.ID)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct{ Data model.Order }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var order model.Order
	f.h.db.First(&order, response.Data.ID)
	if order.UserID != buyer.ID || order.PayableAmount != f.skuRecord.PriceCents || order.AssignedBy != 0 {
		t.Fatalf("self-service privilege escalation: %+v", order)
	}
}
