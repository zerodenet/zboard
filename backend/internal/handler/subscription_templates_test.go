package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateSubscriptionTemplateUsesSystemRenderer(t *testing.T) {
	active := true
	req := subscriptionTemplateWriteReq{
		Name:     "Clash export",
		Slug:     "clash-export",
		Renderer: subscriptionRendererClash,
		IsActive: &active,
	}
	if err := validateSubscriptionTemplate(&req); err != nil {
		t.Fatalf("validateSubscriptionTemplate() error = %v", err)
	}
	rendered, contentType, err := renderSubscriptionWithRenderer(req.Renderer, req.Customization, sampleSubscriptionTemplateData())
	if err != nil {
		t.Fatalf("renderSubscriptionWithRenderer() error = %v", err)
	}
	if contentType != "application/yaml" {
		t.Fatalf("content type = %q, want application/yaml", contentType)
	}
	if !strings.Contains(rendered, "Hong Kong VLESS") {
		t.Fatalf("rendered subscription %q does not contain the sample endpoint", rendered)
	}
}

func TestValidateSubscriptionTemplateRejectsInvalidMetadataAndUnknownRenderer(t *testing.T) {
	tests := []subscriptionTemplateWriteReq{
		{Name: "Bad slug", Slug: "Bad Slug", Renderer: subscriptionRendererClash},
		{Name: "Reserved auto slug", Slug: "auto", Renderer: subscriptionRendererClash},
		{Name: "Reserved native slug", Slug: "native", Renderer: subscriptionRendererClash},
		{Name: "Unknown renderer", Slug: "unknown-renderer", Renderer: "go-template"},
	}
	for _, req := range tests {
		if err := validateSubscriptionTemplate(&req); err == nil {
			t.Fatalf("validateSubscriptionTemplate(%q) succeeded, want error", req.Name)
		}
	}
}

func TestValidateSubscriptionTemplateReturnsFieldErrors(t *testing.T) {
	req := subscriptionTemplateWriteReq{
		Name:        "",
		Slug:        "Bad Slug",
		Description: strings.Repeat("x", 256),
		Renderer:    "go-template",
	}
	err := validateSubscriptionTemplate(&req)
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validateSubscriptionTemplate() error = %#v, want requestValidationError", err)
	}
	for _, field := range []string{"name", "slug", "description", "renderer"} {
		if validation.fields[field] == "" {
			t.Fatalf("validateSubscriptionTemplate() fields = %#v, want %q", validation.fields, field)
		}
	}
}

func TestSubscriptionRendererContentTypesAreBackendOwned(t *testing.T) {
	for renderer, expected := range map[string]string{
		subscriptionRendererZnetSink: "application/json",
		subscriptionRendererClash:    "application/yaml",
		subscriptionRendererSingBox:  "application/json",
	} {
		definition, ok := subscriptionRenderer(renderer)
		if !ok || definition.contentType != expected {
			t.Fatalf("subscriptionRenderer(%q) = %#v, %v; want content type %q", renderer, definition, ok, expected)
		}
	}
}

func TestZnetSinkDeliveryUsesBase64WithoutChangingOtherFormats(t *testing.T) {
	raw := `{"outbounds":[]}`
	encoded, contentType, format := encodeSubscriptionTemplateDelivery(subscriptionRendererZnetSink, raw, "application/json")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != raw || contentType != "text/plain" || format != "znet-sink-base64" {
		t.Fatalf("encoded delivery = (%q, %q, %q)", decoded, contentType, format)
	}

	clash, clashType, clashFormat := encodeSubscriptionTemplateDelivery(subscriptionRendererClash, "proxies: []", "application/yaml")
	if clash != "proxies: []" || clashType != "application/yaml" || clashFormat != subscriptionRendererClash {
		t.Fatalf("Clash delivery unexpectedly changed: (%q, %q, %q)", clash, clashType, clashFormat)
	}
}

func TestNormalizeSubscriptionCustomizationRejectsUnsafeOrIncompatibleRules(t *testing.T) {
	tests := []struct {
		name     string
		renderer string
		raw      string
	}{
		{
			name: "credential in URL", renderer: subscriptionRendererClash,
			raw: `{"version":1,"rule_sets":[{"tag":"ads","url":"https://user:secret@example.com/ads.yaml","behavior":"domain","format":"yaml","action":"reject","interval":3600}]}`,
		},
		{
			name: "classical MRS", renderer: subscriptionRendererClash,
			raw: `{"version":1,"rule_sets":[{"tag":"ads","url":"https://example.com/ads.mrs","behavior":"classical","format":"mrs","action":"reject","interval":3600}]}`,
		},
		{
			name: "managed proxies override", renderer: subscriptionRendererClash,
			raw: `{"version":1,"advanced_source":"proxies: []"}`,
		},
		{
			name: "managed outbounds override", renderer: subscriptionRendererSingBox,
			raw: `{"version":1,"advanced_source":"{\"outbounds\":[]}"}`,
		},
		{
			name: "unknown default profile", renderer: subscriptionRendererClash,
			raw: `{"version":1,"profile":"xboard-source"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizeSubscriptionCustomization(test.renderer, json.RawMessage(test.raw)); err == nil {
				t.Fatal("normalizeSubscriptionCustomization() succeeded, want error")
			}
		})
	}
}

func TestNormalizeSubscriptionCustomizationMigratesLegacyShapeToPolicyGroups(t *testing.T) {
	customization, normalized, err := normalizeSubscriptionCustomization(
		subscriptionRendererClash,
		json.RawMessage(`{"version":1,"group_name":"Legacy","final":"proxy","rule_sets":[]}`),
	)
	if err != nil {
		t.Fatalf("normalizeSubscriptionCustomization() error = %v", err)
	}
	if customization.Version != 2 || customization.MainGroup != "main" || len(customization.PolicyGroups) != 1 || customization.PolicyGroups[0].Name != "Legacy" {
		t.Fatalf("legacy customization = %#v, want one migrated Legacy group", customization)
	}
	if strings.Contains(string(normalized), `"profile"`) || !strings.Contains(string(normalized), `"policy_groups"`) {
		t.Fatalf("normalized customization = %s, want version 2 policy groups without legacy profile", normalized)
	}
}

func TestNormalizeSubscriptionCustomizationMigratesLegacyHTTPSProbeDefault(t *testing.T) {
	customization, normalized, err := normalizeSubscriptionCustomization(
		subscriptionRendererClash,
		json.RawMessage(`{"version":2,"main_group":"main","policy_groups":[{"id":"main","name":"Main","type":"select","include_groups":["auto"],"default_group":"auto"},{"id":"auto","name":"Auto","type":"urltest","probe_url":"https://www.gstatic.com/generate_204","interval":300}],"final":"group:main","rule_sets":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := customization.PolicyGroups[1].ProbeURL; got != defaultSubscriptionProbeURL {
		t.Fatalf("probe URL = %q, want %q", got, defaultSubscriptionProbeURL)
	}
	if strings.Contains(string(normalized), legacyDefaultSubscriptionProbeURL) || !strings.Contains(string(normalized), defaultSubscriptionProbeURL) {
		t.Fatalf("normalized customization did not migrate HTTP probe default: %s", normalized)
	}
}

func TestNormalizeSubscriptionCustomizationRejectsInvalidGroupRegexAndReferences(t *testing.T) {
	tests := []string{
		`{"version":2,"main_group":"main","policy_groups":[{"id":"main","name":"主策略","type":"select","include_pattern":"["}],"final":"group:main","rule_sets":[]}`,
		`{"version":2,"main_group":"main","policy_groups":[{"id":"main","name":"主策略","type":"select","include_groups":["missing"]}],"final":"group:main","rule_sets":[]}`,
		`{"version":2,"main_group":"main","policy_groups":[{"id":"main","name":"主策略","type":"select"}],"final":"group:missing","rule_sets":[]}`,
	}
	for _, raw := range tests {
		if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererClash, json.RawMessage(raw)); err == nil {
			t.Fatalf("normalizeSubscriptionCustomization(%s) succeeded, want error", raw)
		}
	}
}

func TestTruncateTemplatePreviewPreservesUTF8(t *testing.T) {
	content := strings.Repeat("界", maxTemplatePreviewBytes/3+10)
	truncated, wasTruncated := truncateTemplatePreview(content)
	if !wasTruncated {
		t.Fatal("truncateTemplatePreview() did not report truncation")
	}
	if len(truncated) > maxTemplatePreviewBytes {
		t.Fatalf("truncateTemplatePreview() returned %d bytes, want <= %d", len(truncated), maxTemplatePreviewBytes)
	}
	if !utf8.ValidString(truncated) {
		t.Fatal("truncateTemplatePreview() split a UTF-8 rune")
	}
}

func TestOperationTaskStatusRoundTrip(t *testing.T) {
	for status, code := range map[string]int16{"queued": 0, "running": 1, "succeeded": 2, "failed": 3} {
		if got := operationTaskStatus(status); got != code {
			t.Errorf("operationTaskStatus(%q) = %d, want %d", status, got, code)
		}
		if got := normalizeTaskStatus(code); got != status {
			t.Errorf("normalizeTaskStatus(%d) = %q, want %q", code, got, status)
		}
	}
}
