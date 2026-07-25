package handler

import (
	"encoding/json"
	"errors"
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
	if view.Input.Control != "password" || view.Input.MaxBytes == nil || *view.Input.MaxBytes != 4096 {
		t.Fatalf("secret input schema = %+v, want bounded password control", view.Input)
	}
}

func TestSystemConfigInputSchemas(t *testing.T) {
	tests := []struct {
		key       string
		valueType string
		control   string
		min       int64
		max       int64
		options   int
	}{
		{key: "task_email_enabled", valueType: "bool", control: "switch"},
		{key: "smtp_port", valueType: "int", control: "port", min: 1, max: 65535},
		{key: "smtp_tls_mode", valueType: "string", control: "select", options: 2},
		{key: "smtp_from", valueType: "string", control: "email"},
		{key: "site_desc", valueType: "string", control: "textarea"},
		{key: "unknown_json", valueType: "json", control: "json"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			schema := systemConfigInputSchemaFor(model.SystemConfig{ConfigKey: test.key, ValueType: test.valueType})
			if schema.Control != test.control {
				t.Fatalf("schema control = %q, want %q", schema.Control, test.control)
			}
			if test.min != 0 && (schema.Min == nil || *schema.Min != test.min) {
				t.Fatalf("schema min = %v, want %d", schema.Min, test.min)
			}
			if test.max != 0 && (schema.Max == nil || *schema.Max != test.max) {
				t.Fatalf("schema max = %v, want %d", schema.Max, test.max)
			}
			if len(schema.Options) != test.options {
				t.Fatalf("schema options = %d, want %d", len(schema.Options), test.options)
			}
		})
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
		"smtp_host":     "smtp..example.com",
		"smtp_port":     "70000",
		"smtp_from":     "not-an-email",
		"smtp_tls_mode": "plain",
		"site_logo":     "https://example.com/" + strings.Repeat("a", 2048),
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

func TestValidateTaskContentReturnsFieldErrors(t *testing.T) {
	tests := []struct {
		name   string
		typeID string
		raw    json.RawMessage
		fields []string
	}{
		{name: "quota", typeID: taskTypeQuota, raw: json.RawMessage(`{"delta_mb":0,"reason":"x"}`), fields: []string{"content.delta_mb", "content.reason"}},
		{name: "email", typeID: taskTypeEmail, raw: json.RawMessage(`{"subject":"bad\nsubject","body":""}`), fields: []string{"content.subject", "content.body"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTaskContent(test.typeID, test.raw)
			var validation *requestValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("validateTaskContent() error = %#v, want requestValidationError", err)
			}
			for _, field := range test.fields {
				if validation.fields[field] == "" {
					t.Fatalf("validateTaskContent() fields = %#v, want %q", validation.fields, field)
				}
			}
		})
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
