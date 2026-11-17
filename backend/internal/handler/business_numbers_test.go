package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/businessref"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBusinessNumbersCoverAllOrderCreationPaths(t *testing.T) {
	f := newOrderFixture(t)
	purchase := f.create(t, 0)
	topup := createTopUp(t, f, 600, "numbered-topup")
	request := assignmentRequest(f, 1)
	assigned, e := f.h.services.OrderAssignment.Assign(t.Context(), 1, request)
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := f.h.services.OrderAssignment.Assign(t.Context(), 1, request)
	if e != nil || repeated.ID != assigned.ID || repeated.TradeNo != assigned.TradeNo {
		t.Fatal("assignment number/idempotency changed", repeated, e)
	}
	for _, id := range []uint{purchase.ID, topup.ID, assigned.ID} {
		var row model.Order
		if e := f.h.db.First(&row, id).Error; e != nil {
			t.Fatal(e)
		}
		if row.TradeNo != businessref.Order(id, row.CreatedAt) {
			t.Fatal("persisted order reference is not canonical", row.TradeNo)
		}
		view, e := f.h.services.OrderQueries.Detail(t.Context(), 1, id)
		if e != nil || view.OrderNo != row.TradeNo {
			t.Fatal(view.OrderNo, e)
		}
	}
}

func TestLegacyBusinessNumbersSupportSearchWithoutRewritingPaymentIdentity(t *testing.T) {
	f := newOrderFixture(t)
	order := f.create(t, 0)
	legacy := "2bc90874-5ea8-4c3a-9e93-98bd06512612"
	if e := f.h.db.Model(&model.Order{}).Where("id=?", order.ID).UpdateColumn("trade_no", legacy).Error; e != nil {
		t.Fatal(e)
	}
	for _, search := range []string{businessref.Order(order.ID, order.CreatedAt), legacy} {
		page, e := f.h.services.OrderQueries.Administrative(t.Context(), 1, commerce.OrderQuery{Search: search})
		if e != nil || len(page.Items) != 1 || page.Items[0].ID != order.ID || page.Items[0].OrderNo != businessref.Order(order.ID, order.CreatedAt) {
			t.Fatal(search, page, e)
		}
	}
	w := httptest.NewRecorder()
	f.h.OrderListHandler(w, announcementRequest("GET", "/api/v1/orders?id="+businessref.Order(order.ID, order.CreatedAt)+"&offset=0&limit=25", f.token, ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	outsider := assignmentBuyer(t, f)
	owned, e := f.h.services.OrderQueries.Owned(t.Context(), outsider.ID, commerce.OrderQuery{ID: order.ID})
	if e != nil || len(owned.Items) != 0 {
		t.Fatal("order number escaped owner scope", owned, e)
	}
	references := observability.EntityReferenceData{Orders: map[string]observability.EntityReference{}}
	if e := f.h.services.EntityReferences.Resolve(t.Context(), observability.EntityReferenceRequest{Orders: []uint{order.ID}}, references); e != nil {
		t.Fatal(e)
	}
	display := references.Orders[fmt.Sprint(order.ID)]
	if !strings.Contains(display.Secondary, businessref.Order(order.ID, order.CreatedAt)) || strings.Contains(display.Secondary, legacy) {
		t.Fatal("operator references expose legacy UUID instead of public number", display)
	}
	var row model.Order
	f.h.db.First(&row, order.ID)
	if row.TradeNo != legacy {
		t.Fatal("reading history replaced an existing payment reference")
	}
	// The number stays visible in raw compatibility responses as well as DTOs.
	body, e := json.Marshal(commerce.Order(row))
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	json.Unmarshal(body, &out)
	if out["order_no"] != businessref.Order(row.ID, row.CreatedAt) {
		t.Fatal(string(body))
	}
}

func TestTicketBusinessNumbersPreserveAliasesAndOwnerScope(t *testing.T) {
	f := newOrderFixture(t)
	input := experience.NewTicket{Subject: "付款核对", Category: "billing", Priority: 1, Body: "请核对付款记录"}
	id, e := f.h.services.Tickets.Create(t.Context(), 1, input, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	var created model.Ticket
	if e := f.h.db.First(&created, id).Error; e != nil || created.TicketNo != businessref.Ticket(id, created.CreatedAt) {
		t.Fatal(created.TicketNo, e)
	}
	legacy := model.Ticket{UserID: 1, TicketNo: "T20260719-AB12CD34", Subject: "历史工单", Category: "billing", Priority: 1, Status: "open", LastMessageAt: time.Now().UTC()}
	if e := f.h.db.Create(&legacy).Error; e != nil {
		t.Fatal(e)
	}
	for _, search := range []string{businessref.Ticket(legacy.ID, legacy.CreatedAt), legacy.TicketNo} {
		page, e := f.h.services.Tickets.List(t.Context(), experience.TicketQuery{ActorID: 1, Search: search, Limit: 25})
		if e != nil || len(page.Items) != 1 || page.Items[0].TicketNo != businessref.Ticket(legacy.ID, legacy.CreatedAt) {
			t.Fatal(search, page, e)
		}
	}
	outsider := assignmentBuyer(t, f)
	page, e := f.h.services.Tickets.List(t.Context(), experience.TicketQuery{ActorID: outsider.ID, Search: businessref.Ticket(legacy.ID, legacy.CreatedAt), Limit: 25})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("public number bypassed owner scope", page, e)
	}
	var stored model.Ticket
	f.h.db.First(&stored, legacy.ID)
	if stored.TicketNo != legacy.TicketNo {
		t.Fatal("legacy ticket alias was rewritten")
	}
}
func TestBusinessNumberPathAliasesAreDomainBound(t *testing.T) {
	for _, c := range []struct {
		resource, number string
		ok               bool
	}{{"orders", "O0000000048", true}, {"tickets", "T0000000012", true}, {"users", "O0000000048", false}, {"orders", "T0000000048", false}, {"tickets", "O0000000012", false}} {
		path := fmt.Sprintf("/api/v1/admin/%s/%s", c.resource, c.number)
		id, e := parsePathID(path, "/api/v1/admin/"+c.resource+"/")
		if (e == nil) != c.ok || (c.ok && id == 0) {
			t.Fatal(path, id, e)
		}
	}
}

func TestLegacyBusinessNumberAssignmentReplayKeepsOriginalMerchantIdentity(t *testing.T) {
	f := newOrderFixture(t)
	request := assignmentRequest(f, 1)
	assigned, e := f.h.services.OrderAssignment.Assign(t.Context(), 1, request)
	if e != nil {
		t.Fatal(e)
	}
	legacy := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("admin-assign:%d:%s", 1, request.RequestID))))
	if e := f.h.db.Model(&model.Order{}).Where("id=?", assigned.ID).Updates(map[string]any{"creation_key": nil, "creation_fingerprint": "", "trade_no": legacy}).Error; e != nil {
		t.Fatal(e)
	}
	replay, e := f.h.services.OrderAssignment.Assign(t.Context(), 1, request)
	if e != nil || replay.ID != assigned.ID || replay.TradeNo != legacy {
		t.Fatal("legacy assignment identity changed", replay.ID, e)
	}
	var count int64
	f.h.db.Model(&model.Order{}).Count(&count)
	if count != 1 {
		t.Fatal("legacy replay created a duplicate order", count)
	}
}
