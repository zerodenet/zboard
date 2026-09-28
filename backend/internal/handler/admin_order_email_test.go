package handler

import (
	"context"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"strings"
	"testing"
)

func TestAdministrativeOrdersSearchEmailBeforePaginationAndKeepOwnedProjectionPrivate(t *testing.T) {
	f := newOrderFixture(t)
	buyer := assignmentBuyer(t, f)
	email := "mail-search@example.test"
	if err := f.h.db.Model(&buyer).Update("email", email).Error; err != nil {
		t.Fatal(err)
	}
	var ids []uint
	for i := 0; i < 2; i++ {
		req := assignmentRequest(f, buyer.ID)
		order, err := f.h.services.OrderAssignment.Assign(context.Background(), 1, req)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, order.ID)
	}
	f.create(t, 0)
	page, err := f.h.services.OrderQueries.Administrative(context.Background(), 1, commerce.OrderQuery{Search: "MAIL-SEARCH@", Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != ids[0] || page.Items[0].UserEmail != email {
		t.Fatalf("email page: %+v %v", page, err)
	}
	page, err = f.h.services.OrderQueries.Owned(context.Background(), buyer.ID, commerce.OrderQuery{})
	if err != nil || page.Total != 2 {
		t.Fatalf("owned page: %+v %v", page, err)
	}
	raw, _ := json.Marshal(page.Items)
	if strings.Contains(string(raw), "user_email") || strings.Contains(string(raw), email) {
		t.Fatalf("owned projection exposed email: %s", raw)
	}
}
