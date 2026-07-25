package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAuditLogSummaryOmitsDetailAndUserID(t *testing.T) {
	item := model.AuditLog{
		ID: 12, UserID: 9, Actor: "operator@example.invalid",
		Action: "order.pay", Target: "order:44",
		Detail:    "provider_reference=visible-only-on-demand",
		CreatedAt: time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(newAuditLogSummary(item))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{`"detail":`, `"user_id":`, "visible-only-on-demand"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("audit summary leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"id":12`, `"action":"order.pay"`, `"target":"order:44"`, `"has_detail":true`} {
		if !strings.Contains(text, required) {
			t.Errorf("audit summary is missing %q: %s", required, text)
		}
	}
}

func TestAuditLogDetailRedactsSensitiveAssignments(t *testing.T) {
	item := model.AuditLog{
		ID: 13,
		Detail: `changed=email password=hunter2 token:"private-token" ` +
			`{"authorization":"Bearer abc.def.ghi","safe":"retained"}`,
	}
	payload, err := json.Marshal(newAuditLogDetail(item))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"hunter2", "private-token", "abc.def.ghi"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("audit detail leaked sensitive value %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "[redacted]") || !strings.Contains(text, "retained") {
		t.Fatalf("audit detail did not preserve safe context and redaction markers: %s", text)
	}
}

func TestAuditLogDetailTruncationPreservesUTF8(t *testing.T) {
	value := sanitizeAuditDetail(strings.Repeat("审", auditDetailMaxBytes))
	if !utf8.ValidString(value) {
		t.Fatal("sanitized audit detail is not valid UTF-8")
	}
	if len(value) > auditDetailMaxBytes+len("…") {
		t.Fatalf("sanitized audit detail is too large: %d bytes", len(value))
	}
}
