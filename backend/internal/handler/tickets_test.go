package handler

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeTicketCreateRequest(t *testing.T) {
	body := ticketCreateRequest{
		Subject:  "  无法连接节点  ",
		Category: "connection",
		Body:     "  客户端显示连接超时。  ",
	}
	if err := normalizeTicketCreateRequest(&body); err != nil {
		t.Fatal(err)
	}
	if body.Subject != "无法连接节点" || body.Body != "客户端显示连接超时。" || body.Priority != 1 {
		t.Fatalf("normalized request = %+v", body)
	}
}

func TestNormalizeTicketCreateRequestRejectsInvalidValues(t *testing.T) {
	tests := []ticketCreateRequest{
		{Subject: "", Category: "connection", Body: "details", Priority: 1},
		{Subject: "subject", Category: "unknown", Body: "details", Priority: 1},
		{Subject: "subject", Category: "other", Body: "", Priority: 1},
		{Subject: "subject", Category: "other", Body: "details", Priority: 3},
		{Subject: strings.Repeat("问", 161), Category: "other", Body: "details", Priority: 1},
	}
	for index, body := range tests {
		if err := normalizeTicketCreateRequest(&body); err == nil {
			t.Errorf("case %d: expected validation error", index)
		}
	}
}

func TestTicketEnumsAndNumber(t *testing.T) {
	for _, status := range []string{"open", "pending_admin", "pending_user", "resolved", "closed"} {
		if !validTicketStatus(status) {
			t.Errorf("status %q should be valid", status)
		}
	}
	if validTicketStatus("processing") {
		t.Fatal("unexpected ticket status accepted")
	}
	for _, category := range []string{"connection", "billing", "account", "other"} {
		if !validTicketCategory(category) {
			t.Errorf("category %q should be valid", category)
		}
	}
	number := newTicketNumber(time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(number, "T20260719-") || len(number) != 18 {
		t.Fatalf("ticket number = %q", number)
	}
}
