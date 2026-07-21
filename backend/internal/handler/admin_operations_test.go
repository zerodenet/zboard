package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNormalizeSystemConfigValue(t *testing.T) {
	tests := []struct {
		name      string
		valueType string
		raw       string
		want      string
		wantErr   bool
	}{
		{name: "string", valueType: "string", raw: `"  panel  "`, want: "panel"},
		{name: "bool", valueType: "bool", raw: `true`, want: "true"},
		{name: "integer", valueType: "int", raw: `587`, want: "587"},
		{name: "json canonical", valueType: "json", raw: `{ "b": 2, "a": 1 }`, want: `{"a":1,"b":2}`},
		{name: "wrong bool type", valueType: "bool", raw: `"true"`, wantErr: true},
		{name: "unsupported", valueType: "float", raw: `1`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSystemConfigValue(model.SystemConfig{ValueType: test.valueType}, json.RawMessage(test.raw))
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeSystemConfigValue() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("normalizeSystemConfigValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSystemConfigSecretIsRedacted(t *testing.T) {
	view, err := systemConfigToView(model.SystemConfig{
		ConfigKey: "smtp_password", Value: "zboard:v1:ciphertext", ValueType: "string", IsSecret: true, Revision: 2,
	})
	if err != nil {
		t.Fatalf("systemConfigToView() error = %v", err)
	}
	if view.Value != nil || !view.Configured || !view.IsSecret {
		t.Fatalf("secret view = %+v, want redacted configured secret", view)
	}
}

func TestValidateSystemConfigValue(t *testing.T) {
	valid := map[string]string{
		"site_url":      "https://panel.example.com",
		"smtp_host":     "smtp.example.com",
		"smtp_port":     "587",
		"smtp_from":     "noreply@example.com",
		"smtp_tls_mode": "starttls",
	}
	for key, value := range valid {
		if err := validateSystemConfigValue(key, value); err != nil {
			t.Fatalf("validateSystemConfigValue(%q, %q) error = %v", key, value, err)
		}
	}
	invalid := map[string]string{
		"site_url":      "javascript:alert(1)",
		"smtp_host":     "smtp.example.com:587",
		"smtp_port":     "70000",
		"smtp_from":     "not-an-email",
		"smtp_tls_mode": "plain",
	}
	for key, value := range invalid {
		if err := validateSystemConfigValue(key, value); err == nil {
			t.Fatalf("validateSystemConfigValue(%q, %q) expected error", key, value)
		}
	}
}

func TestValidateTaskContent(t *testing.T) {
	if err := validateTaskContent(taskTypeQuota, json.RawMessage(`{"delta_mb":1024,"reason":"manual correction"}`)); err != nil {
		t.Fatalf("valid quota content error = %v", err)
	}
	if err := validateTaskContent(taskTypeQuota, json.RawMessage(`{"delta_mb":0,"reason":"manual correction"}`)); err == nil {
		t.Fatal("zero quota adjustment should fail")
	}
	if err := validateTaskContent(taskTypeEmail, json.RawMessage(`{"subject":"Maintenance","body":"Window starts at 02:00 UTC."}`)); err != nil {
		t.Fatalf("valid email content error = %v", err)
	}
	if err := validateTaskContent(taskTypeEmail, json.RawMessage("{\"subject\":\"bad\\r\\nBcc: victim@example.com\",\"body\":\"x\"}")); err == nil {
		t.Fatal("email header injection should fail")
	}
}

func TestNewTaskItemUsesValidJSONPayload(t *testing.T) {
	item := newTaskItem("subscription", 42)
	if item.TargetType != "subscription" || item.TargetID != "42" || item.Status != taskStatusPending {
		t.Fatalf("task item = %+v", item)
	}
	if !json.Valid([]byte(item.Payload)) {
		t.Fatalf("task item payload = %q, want valid JSON", item.Payload)
	}
}

func TestParseTaskRunID(t *testing.T) {
	id, err := parseTaskRunID("/api/v1/admin/tasks/42/run")
	if err != nil || id != 42 {
		t.Fatalf("parseTaskRunID() = %d, %v; want 42, nil", id, err)
	}
	if _, err := parseTaskRunID("/api/v1/admin/tasks/42"); err == nil {
		t.Fatal("path without run action should fail")
	}
}

func TestBuildSMTPMessage(t *testing.T) {
	message := buildSMTPMessage("noreply@example.com", "user@example.com", "维护通知", "body", "<task-1@example.com>")
	for _, expected := range []string{"From: noreply@example.com", "To: user@example.com", "Subject: =?UTF-8?q?", "Message-ID: <task-1@example.com>", "\r\n\r\nbody"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message does not contain %q: %q", expected, message)
		}
	}
}
