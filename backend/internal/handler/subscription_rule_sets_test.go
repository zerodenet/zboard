package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestValidateSubscriptionRuleSetNormalizesRendererFields(t *testing.T) {
	req := subscriptionRuleSetWriteReq{
		Name: "Advertising", Renderer: subscriptionRendererClash, Tag: "reject-ads",
		URL: "https://example.com/ads.yaml", Interval: 3600,
	}
	if err := validateSubscriptionRuleSet(&req); err != nil {
		t.Fatalf("validateSubscriptionRuleSet() error = %v", err)
	}
	if req.Behavior != "classical" || req.Format != "yaml" {
		t.Fatalf("normalized rule set = %#v", req)
	}
}

func TestValidateSubscriptionRuleSetRejectsUnsafeAndIncompatibleSources(t *testing.T) {
	tests := []subscriptionRuleSetWriteReq{
		{Name: "unsafe", Renderer: subscriptionRendererClash, Tag: "ads", URL: "https://user:secret@example.com/ads.yaml", Interval: 3600},
		{Name: "incompatible", Renderer: subscriptionRendererClash, Tag: "ads", URL: "https://example.com/ads.mrs", Behavior: "classical", Format: "mrs", Interval: 3600},
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

func TestResolveSubscriptionCustomizationUsesMaintainedSourceAndTemplateTarget(t *testing.T) {
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
