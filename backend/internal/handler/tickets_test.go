package handler

import (
	"encoding/json"
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
	for _, test := range []struct {
		status     string
		adminScope bool
		want       []string
		valid      bool
	}{
		{status: adminAttentionStatus, adminScope: true, want: []string{ticketStatusOpen, ticketStatusPendingAdmin}, valid: true},
		{status: ticketStatusPendingUser, adminScope: true, want: []string{ticketStatusPendingUser}, valid: true},
		{status: adminAttentionStatus, adminScope: false, valid: false},
	} {
		got, valid := ticketListStatusValues(test.status, test.adminScope)
		if valid != test.valid || len(got) != len(test.want) {
			t.Errorf("ticketListStatusValues(%q, %v) = %#v, %v; want %#v, %v", test.status, test.adminScope, got, valid, test.want, test.valid)
			continue
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Errorf("ticketListStatusValues(%q, %v)[%d] = %q, want %q", test.status, test.adminScope, index, got[index], test.want[index])
			}
		}
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

func TestTicketMessageLimitBounds(t *testing.T) {
	for _, value := range []string{"", "20", "50", "100"} {
		if _, err := parseTicketMessageLimit(value); err != nil {
			t.Errorf("message limit %q should be valid: %v", value, err)
		}
	}
	for _, value := range []string{"0", "19", "101", "invalid"} {
		if _, err := parseTicketMessageLimit(value); err == nil {
			t.Errorf("message limit %q should be rejected", value)
		}
	}
}

func TestTicketDetailExposesBoundedHistoryState(t *testing.T) {
	payload, err := json.Marshal(ticketDetailView{
		Messages:         []ticketMessageView{{}},
		HasOlderMessages: true,
		OldestMessageID:  42,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"has_older_messages":true`, `"oldest_message_id":42`, `"messages":[`} {
		if !strings.Contains(text, expected) {
			t.Errorf("ticket detail is missing %q: %s", expected, text)
		}
	}
}
