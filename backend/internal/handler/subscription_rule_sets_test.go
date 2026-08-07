package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestValidateSubscriptionRuleSetNormalizesManagedFields(t *testing.T) {
	req := subscriptionRuleSetWriteReq{
		Name: "Advertising", Tag: "reject-ads",
		SourceURL: "https://example.com/ads.txt", SourceFormat: "domain_list", SyncInterval: 3600,
	}
	if err := validateSubscriptionRuleSet(&req); err != nil {
		t.Fatalf("validateSubscriptionRuleSet() error = %v", err)
	}
	if req.SourceFormat != managedRuleSourceDomainList || req.SyncInterval != 3600 {
		t.Fatalf("normalized rule set = %#v", req)
	}
}

func TestValidateSubscriptionRuleSetRejectsUnsafeOrInvalidSources(t *testing.T) {
	invalidIR := `{"version":1,"rules":[{"type":"domain_exact","value":"example.com","extra":true}]}`
	tests := []subscriptionRuleSetWriteReq{
		{Name: "unsafe", Tag: "ads", SourceURL: "https://user:secret@example.com/ads.yaml", SourceFormat: "clash_classical", SyncInterval: 3600},
		{Name: "invalid", Tag: "ads", Content: &invalidIR, SourceFormat: managedRuleSourceZeroRuleIR, SyncInterval: 3600},
	}
	for _, req := range tests {
		if err := validateSubscriptionRuleSet(&req); err == nil {
			t.Fatalf("validateSubscriptionRuleSet(%q) succeeded, want error", req.Name)
		}
	}
}

func TestNormalizeSubscriptionCustomizationMigratesLibraryReferences(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"profile":"minimal","group_name":"Proxy","final":"proxy","rule_sets":[{"rule_set_id":7,"action":"reject"}]}`)
	customization, normalized, err := normalizeSubscriptionCustomization(subscriptionRendererClash, raw)
	if err != nil {
		t.Fatalf("normalizeSubscriptionCustomization() error = %v", err)
	}
	if len(customization.RuleSets) != 1 || customization.RuleSets[0].RuleSetID != 7 {
		t.Fatalf("customization = %#v", customization)
	}
	if strings.Contains(string(normalized), `"profile"`) || !strings.Contains(string(normalized), `"target":"reject"`) {
		t.Fatalf("normalized = %s", normalized)
	}
	for _, invalid := range []string{
		`{"version":1,"rule_sets":[{"rule_set_id":7,"tag":"override","action":"proxy"}]}`,
		`{"version":1,"rule_sets":[{"rule_set_id":7,"action":"proxy"},{"rule_set_id":7,"action":"direct"}]}`,
	} {
		if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererClash, json.RawMessage(invalid)); err == nil {
			t.Fatalf("normalizeSubscriptionCustomization(%s) succeeded, want error", invalid)
		}
	}
}

func TestResolveSubscriptionCustomizationUsesMaintainedExternalSourceAndTemplateTarget(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"profile":"minimal","group_name":"Proxy","final":"proxy","rule_sets":[{"rule_set_id":7,"action":"reject"}]}`)
	records := map[uint]model.SubscriptionRuleSet{
		7: {
			ID: 7, Name: "Advertising", Renderer: subscriptionRendererClash, Tag: "reject-ads",
			URL: "https://example.com/ads.yaml", Behavior: "domain", Format: "yaml",
			Interval: 3600, IsActive: true,
		},
	}
	resolved, err := resolveSubscriptionCustomizationWithRecords(subscriptionRendererClash, raw, records, true)
	if err != nil {
		t.Fatalf("resolveSubscriptionCustomizationWithRecords() error = %v", err)
	}
	if strings.Contains(string(resolved), "rule_set_id") || !strings.Contains(string(resolved), `"tag":"reject-ads"`) || !strings.Contains(string(resolved), `"target":"reject"`) {
		t.Fatalf("resolved = %s", resolved)
	}
	rendered, _, err := renderSubscriptionWithRenderer(subscriptionRendererClash, resolved, sampleSubscriptionTemplateData())
	if err != nil {
		t.Fatalf("renderSubscriptionWithRenderer() error = %v", err)
	}
	if !strings.Contains(rendered, "RULE-SET,reject-ads,REJECT") {
		t.Fatalf("rendered output does not contain resolved rule set:\n%s", rendered)
	}
}

func TestResolveManagedRuleSetUsesRendererSpecificArtifact(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"rule_sets":[{"rule_set_id":7,"action":"reject"}]}`)
	records := map[uint]model.SubscriptionRuleSet{
		7: {ID: 7, Name: "Advertising", Renderer: managedRuleSetRenderer, Tag: "reject-ads", Interval: 3600, IsActive: true},
	}
	tests := []struct {
		renderer string
		format   string
		query    string
	}{
		{subscriptionRendererZNetSink, "zero_rule_ir", "format=zero_rule_ir"},
		{subscriptionRendererClash, "yaml", "format=clash-classical-yaml"},
		{subscriptionRendererSingBox, "source", "format=sing-box-source"},
	}
	for _, test := range tests {
		resolved, err := resolveSubscriptionCustomizationWithRecordsAt(test.renderer, raw, records, "https://panel.example.com", true)
		if err != nil {
			t.Fatalf("resolve %s: %v", test.renderer, err)
		}
		if !strings.Contains(string(resolved), `"format":"`+test.format+`"`) || !strings.Contains(string(resolved), test.query) {
			t.Fatalf("resolved %s = %s", test.renderer, resolved)
		}
	}
}

func TestResolveSubscriptionCustomizationRejectsMissingInactiveOrWrongRenderer(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"rule_sets":[{"rule_set_id":7,"action":"proxy"}]}`)
	tests := []map[uint]model.SubscriptionRuleSet{
		{},
		{7: {ID: 7, Name: "Inactive", Renderer: subscriptionRendererClash, IsActive: false}},
		{7: {ID: 7, Name: "Wrong", Renderer: subscriptionRendererSingBox, IsActive: true}},
	}
	for _, records := range tests {
		if _, err := resolveSubscriptionCustomizationWithRecords(subscriptionRendererClash, raw, records, true); err == nil {
			t.Fatalf("resolveSubscriptionCustomizationWithRecords(%#v) succeeded, want error", records)
		}
	}
}
