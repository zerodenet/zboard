package handler

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionSpecialTargetsDefaultAndExplicit(t *testing.T) {
	legacyV2 := json.RawMessage(`{
		"version":2,
		"mixed_port":7890,
		"main_group":"main",
		"policy_groups":[{"id":"main","name":"节点选择","type":"select"}],
		"final":"group:main"
	}`)
	customization, normalized, err := normalizeSubscriptionCustomization(subscriptionRendererClash, legacyV2)
	if err != nil {
		t.Fatalf("normalize legacy v2 customization: %v", err)
	}
	if len(customization.PolicyGroups) != 1 {
		t.Fatalf("expected one policy group, got %d", len(customization.PolicyGroups))
	}
	group := customization.PolicyGroups[0]
	if !subscriptionPolicyGroupIncludesDirect(group) || !subscriptionPolicyGroupIncludesReject(group) {
		t.Fatalf("legacy v2 select group should default DIRECT and REJECT on: %+v", group)
	}
	if !strings.Contains(string(normalized), `"include_direct":true`) || !strings.Contains(string(normalized), `"include_reject":true`) {
		t.Fatalf("normalized customization should persist explicit defaults: %s", normalized)
	}

	explicit := json.RawMessage(`{
		"version":2,
		"mixed_port":7890,
		"main_group":"main",
		"policy_groups":[{"id":"main","name":"节点选择","type":"select","include_direct":false,"include_reject":true}],
		"final":"group:main"
	}`)
	customization, _, err = normalizeSubscriptionCustomization(subscriptionRendererClash, explicit)
	if err != nil {
		t.Fatalf("normalize explicit customization: %v", err)
	}
	group = customization.PolicyGroups[0]
	if subscriptionPolicyGroupIncludesDirect(group) {
		t.Fatal("explicit include_direct=false was not preserved")
	}
	if !subscriptionPolicyGroupIncludesReject(group) {
		t.Fatal("explicit include_reject=true was not preserved")
	}
}

func TestApplySelectionMembersRespectsSpecialTargets(t *testing.T) {
	includeReject := true
	excludeDirect := false
	preferences := map[string]subscriptionPolicyGroup{
		"节点选择": {
			Name:          "节点选择",
			Type:          "select",
			IncludeDirect: &excludeDirect,
			IncludeReject: &includeReject,
		},
	}
	group := map[string]interface{}{
		"name":    "节点选择",
		"proxies": []interface{}{"节点 A", "DIRECT", "REJECT"},
	}
	applySelectionMembers(group, "name", "proxies", preferences)
	members, ok := group["proxies"].([]string)
	if !ok {
		t.Fatalf("filtered members have unexpected type %T", group["proxies"])
	}
	if strings.Join(members, ",") != "节点 A,REJECT" {
		t.Fatalf("unexpected members after special target filtering: %v", members)
	}
}

func TestDetectSubscriptionTemplateForAdditionalClients(t *testing.T) {
	cases := map[string]string{
		"Shadowrocket/2.2.60":         subscriptionRendererShadowrocket,
		"Quantumult X/1.5.0":          subscriptionRendererQuantumultX,
		"QuantumultX build 900":       subscriptionRendererQuantumultX,
		"v2rayN/7.15.4":               subscriptionRendererV2RayN,
		"Mozilla/5.0 Shadowrocket":    subscriptionRendererShadowrocket,
		"Mozilla/5.0 unrelated-agent": "",
	}
	for userAgent, expected := range cases {
		if actual := detectSubscriptionTemplate(userAgent); actual != expected {
			t.Errorf("detectSubscriptionTemplate(%q) = %q, want %q", userAgent, actual, expected)
		}
	}
}

func TestShareLinkSubscriptionRenderer(t *testing.T) {
	data := subscriptionTemplateData{
		ProtocolEndpoints: []subscriptionTemplateEndpoint{
			{
				Name:       "Tokyo VLESS",
				Address:    "edge.example.com",
				Port:       443,
				PublicPort: 443,
				Protocol:   "vless",
				Config: map[string]interface{}{
					"server": "edge.example.com",
					"id":     "11111111-1111-1111-1111-111111111111",
					"tls": map[string]interface{}{
						"server_name": "edge.example.com",
					},
				},
			},
		},
	}
	rendered, err := renderV2RayNSubscription(data, defaultSubscriptionCustomization(subscriptionRendererV2RayN))
	if err != nil {
		t.Fatalf("render v2rayN subscription: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(rendered)
	if err != nil {
		t.Fatalf("decode v2rayN subscription: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "vless://") {
		t.Fatalf("expected VLESS share link, got %q", decoded)
	}
}

func TestQuantumultXSubscriptionRenderer(t *testing.T) {
	data := subscriptionTemplateData{
		ProtocolEndpoints: []subscriptionTemplateEndpoint{
			{
				Name:       "Tokyo SS",
				Address:    "edge.example.com",
				Port:       8388,
				PublicPort: 8388,
				Protocol:   "shadowsocks",
				Config: map[string]interface{}{
					"server":   "edge.example.com",
					"cipher":   "chacha20-ietf-poly1305",
					"password": "secret",
				},
			},
		},
	}
	rendered, err := renderQuantumultXSubscription(data, defaultSubscriptionCustomization(subscriptionRendererQuantumultX))
	if err != nil {
		t.Fatalf("render Quantumult X subscription: %v", err)
	}
	for _, expected := range []string{
		"shadowsocks=edge.example.com:8388",
		"method=chacha20-ietf-poly1305",
		"password=secret",
		"tag=Tokyo SS",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("Quantumult X output missing %q: %s", expected, rendered)
		}
	}
}

func TestQuantumultXRendererAllowsNoCompatibleNodes(t *testing.T) {
	data := subscriptionTemplateData{
		ProtocolEndpoints: []subscriptionTemplateEndpoint{{Name: "Mieru", Protocol: "mieru"}},
	}
	rendered, err := renderQuantumultXSubscription(data, defaultSubscriptionCustomization(subscriptionRendererQuantumultX))
	if err != nil {
		t.Fatalf("empty compatible Quantumult X output should not invalidate template: %v", err)
	}
	if !strings.Contains(rendered, "没有 Quantumult X 兼容节点") {
		t.Fatalf("unexpected empty compatibility output: %q", rendered)
	}
}
