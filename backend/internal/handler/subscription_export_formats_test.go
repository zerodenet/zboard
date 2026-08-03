package handler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestBuiltInSubscriptionExportersRenderTargetSchemas(t *testing.T) {
	data := subscriptionExporterTestData()
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererClash, subscriptionRendererSingBox} {
		rendered, contentType, err := renderSubscriptionWithRenderer(renderer, nil, data)
		if err != nil {
			t.Fatalf("renderSubscriptionWithRenderer(%q) error = %v", renderer, err)
		}
		if strings.TrimSpace(rendered) == "" || contentType == "" {
			t.Fatalf("renderSubscriptionWithRenderer(%q) returned content=%q contentType=%q", renderer, rendered, contentType)
		}
	}
}

func TestSubscriptionEndpointTagPreservesOriginalName(t *testing.T) {
	endpoint := subscriptionTemplateEndpoint{ID: 1, SubscriptionID: 1, Name: "本机 Shadowsocks", Protocol: "shadowsocks"}
	if got := subscriptionEndpointTag(endpoint); got != "本机 Shadowsocks" {
		t.Fatalf("subscriptionEndpointTag() = %q, want original name", got)
	}
	endpoint.Name = ""
	if got := subscriptionEndpointTag(endpoint); got != "SHADOWSOCKS" {
		t.Fatalf("blank-name subscriptionEndpointTag() = %q, want protocol fallback", got)
	}
}

func TestManualSelectionGroupsKeepDirectAndRejectPermanent(t *testing.T) {
	data := subscriptionExporterTestData()

	zeroRendered, err := renderZnetSinkSubscription(data, defaultSubscriptionCustomization(subscriptionRendererZnetSink))
	if err != nil {
		t.Fatal(err)
	}
	var zeroDocument struct {
		OutboundGroups []struct {
			Type      string   `json:"type"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbound_groups"`
	}
	if err := json.Unmarshal([]byte(zeroRendered), &zeroDocument); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(zeroDocument.OutboundGroups[0].Outbounds, "DIRECT") || !slices.Contains(zeroDocument.OutboundGroups[0].Outbounds, "REJECT") {
		t.Fatalf("Zero selector permanent targets = %#v", zeroDocument.OutboundGroups[0].Outbounds)
	}
	if slices.Contains(zeroDocument.OutboundGroups[1].Outbounds, "DIRECT") || slices.Contains(zeroDocument.OutboundGroups[1].Outbounds, "REJECT") {
		t.Fatalf("Zero url-test contains non-probe targets: %#v", zeroDocument.OutboundGroups[1].Outbounds)
	}

	clashRendered, err := renderClashSubscription(data, defaultSubscriptionCustomization(subscriptionRendererClash))
	if err != nil {
		t.Fatal(err)
	}
	var clashDocument clashSubscriptionDocument
	if err := yaml.Unmarshal([]byte(clashRendered), &clashDocument); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(clashDocument.ProxyGroups[0].Proxies, "DIRECT") || !slices.Contains(clashDocument.ProxyGroups[0].Proxies, "REJECT") {
		t.Fatalf("Clash select permanent targets = %#v", clashDocument.ProxyGroups[0].Proxies)
	}
	if slices.Contains(clashDocument.ProxyGroups[1].Proxies, "DIRECT") || slices.Contains(clashDocument.ProxyGroups[1].Proxies, "REJECT") {
		t.Fatalf("Clash url-test contains non-probe targets: %#v", clashDocument.ProxyGroups[1].Proxies)
	}

	singRendered, err := renderSingBoxSubscription(data, defaultSubscriptionCustomization(subscriptionRendererSingBox))
	if err != nil {
		t.Fatal(err)
	}
	var singDocument struct {
		Outbounds []struct {
			Type      string   `json:"type"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(singRendered), &singDocument); err != nil {
		t.Fatal(err)
	}
	for _, outbound := range singDocument.Outbounds {
		switch outbound.Type {
		case "selector":
			if !slices.Contains(outbound.Outbounds, "DIRECT") || !slices.Contains(outbound.Outbounds, "REJECT") {
				t.Fatalf("sing-box selector permanent targets = %#v", outbound.Outbounds)
			}
		case "urltest":
			if slices.Contains(outbound.Outbounds, "DIRECT") || slices.Contains(outbound.Outbounds, "REJECT") {
				t.Fatalf("sing-box urltest contains non-probe targets: %#v", outbound.Outbounds)
			}
		}
	}
}

func TestRenderZnetSinkSubscriptionProducesZeroConfig(t *testing.T) {
	rendered, err := renderZnetSinkSubscription(subscriptionExporterTestData(), defaultSubscriptionCustomization(subscriptionRendererZnetSink))
	if err != nil {
		t.Fatalf("renderZnetSinkSubscription() error = %v", err)
	}
	var document struct {
		Outbounds []struct {
			Tag      string                 `json:"tag"`
			Protocol map[string]interface{} `json:"protocol"`
		} `json:"outbounds"`
		OutboundGroups []struct {
			Tag       string   `json:"tag"`
			Type      string   `json:"type"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbound_groups"`
		Route struct {
			Final map[string]interface{} `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("rendered ZNet Sink output is not JSON: %v\n%s", err, rendered)
	}
	if len(document.Outbounds) != 10 {
		t.Fatalf("outbounds count = %d, want 10", len(document.Outbounds))
	}
	if document.Outbounds[0].Protocol["type"] != "direct" || document.Outbounds[4].Protocol["type"] != "vless" {
		t.Fatalf("unexpected zero outbounds = %#v", document.Outbounds)
	}
	if document.Outbounds[4].Protocol["server"] != "vless.example.com" || document.Outbounds[4].Protocol["port"] != float64(443) {
		t.Fatalf("VLESS server contract = %#v", document.Outbounds[4].Protocol)
	}
	zeroReality, ok := document.Outbounds[4].Protocol["reality"].(map[string]interface{})
	if !ok || zeroReality["client_fingerprint"] != "firefox" {
		t.Fatalf("Zero VLESS Reality = %#v", document.Outbounds[4].Protocol["reality"])
	}
	if len(document.OutboundGroups) != 2 || document.OutboundGroups[0].Type != "selector" || len(document.OutboundGroups[0].Outbounds) != 9 || document.OutboundGroups[1].Type != "url_test" {
		t.Fatalf("outbound_groups = %#v", document.OutboundGroups)
	}
	if document.Route.Final["outbound"] != "节点选择" {
		t.Fatalf("route.final = %#v", document.Route.Final)
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN")); validator != "" {
		configPath := filepath.Join(t.TempDir(), "znet-sink-subscription.json")
		if err := os.WriteFile(configPath, []byte(rendered), 0o600); err != nil {
			t.Fatalf("write Zero validation fixture: %v", err)
		}
		output, err := exec.Command(validator, "validate", configPath).CombinedOutput()
		if err != nil {
			t.Fatalf("zero validate failed: %v\n%s\n%s", err, output, rendered)
		}
	}
}

func TestRenderClashSubscriptionConvertsProtocolFields(t *testing.T) {
	rendered, err := renderClashSubscription(subscriptionExporterTestData(), defaultSubscriptionCustomization(subscriptionRendererClash))
	if err != nil {
		t.Fatalf("renderClashSubscription() error = %v", err)
	}
	var document clashSubscriptionDocument
	if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("rendered Clash output is not YAML: %v\n%s", err, rendered)
	}
	if len(document.Proxies) != 6 {
		t.Fatalf("proxies count = %d, want 6", len(document.Proxies))
	}
	vless := findExportByType(t, document.Proxies, "vless")
	if vless["uuid"] != "11111111-1111-4111-8111-111111111111" || vless["tls"] != true {
		t.Fatalf("Clash VLESS = %#v", vless)
	}
	if vless["client-fingerprint"] != "firefox" {
		t.Fatalf("Clash VLESS Reality fingerprint = %#v", vless)
	}
	if _, leaked := vless["id"]; leaked {
		t.Fatalf("Clash VLESS leaked Zero id field: %#v", vless)
	}
	vmess := findExportByType(t, document.Proxies, "vmess")
	if vmess["network"] != "ws" {
		t.Fatalf("Clash VMess transport = %#v", vmess)
	}
	shadowsocks := findExportByType(t, document.Proxies, "ss")
	if shadowsocks["cipher"] != "aes-128-gcm" || shadowsocks["password"] != "ss-secret" {
		t.Fatalf("Clash Shadowsocks = %#v", shadowsocks)
	}
	mieru := findExportByType(t, document.Proxies, "mieru")
	if mieru["transport"] != "TCP" || mieru["username"] != "subscriber" {
		t.Fatalf("Clash Mieru = %#v", mieru)
	}
	if len(document.ProxyGroups) != 2 || document.ProxyGroups[0].Type != "select" || len(document.ProxyGroups[0].Proxies) != 9 || document.ProxyGroups[1].Type != "url-test" {
		t.Fatalf("proxy-groups = %#v", document.ProxyGroups)
	}
	if len(document.Rules) != 1 || document.Rules[0] != "MATCH,节点选择" {
		t.Fatalf("rules = %#v", document.Rules)
	}
}

func TestCanonicalGrpcServiceNamesReachClashAndSingBox(t *testing.T) {
	config := map[string]interface{}{"service_names": []interface{}{"managed-edge"}}
	clash := map[string]interface{}{}
	addClashTransport(clash, map[string]interface{}{"grpc": config})
	options, ok := clash["grpc-opts"].(map[string]interface{})
	if !ok || options["grpc-service-name"] != "managed-edge" {
		t.Fatalf("Clash gRPC options = %#v", clash["grpc-opts"])
	}
	singBox := singBoxTransport(map[string]interface{}{"grpc": config})
	if singBox["type"] != "grpc" || singBox["service_name"] != "managed-edge" {
		t.Fatalf("sing-box gRPC transport = %#v", singBox)
	}
}

func TestRenderSingBoxSubscriptionConvertsAndOmitsUnsupportedMieru(t *testing.T) {
	rendered, err := renderSingBoxSubscription(subscriptionExporterTestData(), defaultSubscriptionCustomization(subscriptionRendererSingBox))
	if err != nil {
		t.Fatalf("renderSingBoxSubscription() error = %v", err)
	}
	var document struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
		Route     map[string]interface{}   `json:"route"`
	}
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("rendered sing-box output is not JSON: %v\n%s", err, rendered)
	}
	if len(document.Outbounds) != 10 {
		t.Fatalf("outbounds count = %d, want 10", len(document.Outbounds))
	}
	for _, outbound := range document.Outbounds {
		if outbound["type"] == "mieru" {
			t.Fatalf("sing-box output contains unsupported Mieru outbound: %#v", outbound)
		}
	}
	vless := findExportByType(t, document.Outbounds, "vless")
	if vless["uuid"] != "11111111-1111-4111-8111-111111111111" || vless["server_port"] != float64(443) {
		t.Fatalf("sing-box VLESS = %#v", vless)
	}
	tls, ok := vless["tls"].(map[string]interface{})
	if !ok || tls["enabled"] != true {
		t.Fatalf("sing-box VLESS TLS = %#v", vless["tls"])
	}
	reality, ok := tls["reality"].(map[string]interface{})
	if !ok || reality["public_key"] != "9AwHi13y1rN6EWTSo8-HNCOhrzr251jNY7SSIxo0diA" || reality["short_id"] != "0123456789abcdef" {
		t.Fatalf("sing-box VLESS Reality = %#v", tls["reality"])
	}
	utls, ok := tls["utls"].(map[string]interface{})
	if !ok || utls["enabled"] != true || utls["fingerprint"] != "firefox" {
		t.Fatalf("sing-box VLESS Reality uTLS = %#v", tls["utls"])
	}
	vmess := findExportByType(t, document.Outbounds, "vmess")
	transport, ok := vmess["transport"].(map[string]interface{})
	if !ok || transport["type"] != "ws" || transport["path"] != "/subscription" {
		t.Fatalf("sing-box VMess transport = %#v", vmess["transport"])
	}
	if document.Route["final"] != "节点选择" {
		t.Fatalf("route = %#v", document.Route)
	}
}

func TestPolicyGroupsRenderNativeTypesAndNodeNameFilters(t *testing.T) {
	data := subscriptionExporterTestData()

	clashCustomization := defaultSubscriptionCustomization(subscriptionRendererClash)
	clashCustomization.PolicyGroups[1].IncludePattern = `^(Hong Kong VLESS|Tokyo VMess)$`
	clashCustomization.PolicyGroups = append(clashCustomization.PolicyGroups, subscriptionPolicyGroup{
		ID: "failover", Name: "故障转移", Type: "fallback",
		IncludePattern: `Singapore|Los Angeles`, ProbeURL: "http://www.gstatic.com/generate_204", Interval: 300,
	})
	clashCustomization.PolicyGroups[0].IncludeGroups = append(clashCustomization.PolicyGroups[0].IncludeGroups, "failover")
	clashRendered, err := renderClashSubscription(data, clashCustomization)
	if err != nil {
		t.Fatalf("renderClashSubscription() policy groups error = %v", err)
	}
	var clashDocument clashSubscriptionDocument
	if err := yaml.Unmarshal([]byte(clashRendered), &clashDocument); err != nil {
		t.Fatalf("policy-group Clash output is not YAML: %v\n%s", err, clashRendered)
	}
	if len(clashDocument.ProxyGroups) != 3 || clashDocument.ProxyGroups[1].Type != "url-test" || clashDocument.ProxyGroups[2].Type != "fallback" {
		t.Fatalf("Clash groups = %#v", clashDocument.ProxyGroups)
	}
	if len(clashDocument.ProxyGroups[1].Proxies) != 2 || !strings.Contains(strings.Join(clashDocument.ProxyGroups[1].Proxies, ","), "Hong Kong VLESS") {
		t.Fatalf("Clash regex-filtered url-test group = %#v", clashDocument.ProxyGroups[1])
	}
	if clashDocument.ProxyGroups[0].Proxies[0] != "自动选择" || clashDocument.ProxyGroups[0].Proxies[1] != "故障转移" {
		t.Fatalf("Clash main group references = %#v", clashDocument.ProxyGroups[0])
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_MIHOMO_VALIDATE_BIN")); validator != "" {
		configDir := t.TempDir()
		configPath := filepath.Join(configDir, "policy-groups-clash.yaml")
		if err := os.WriteFile(configPath, []byte(clashRendered), 0o600); err != nil {
			t.Fatalf("write balanced Clash validation fixture: %v", err)
		}
		output, err := exec.Command(validator, "-t", "-f", configPath, "-d", configDir).CombinedOutput()
		if err != nil {
			t.Fatalf("balanced Mihomo validation failed: %v\n%s\n%s", err, output, clashRendered)
		}
	}

	singCustomization := defaultSubscriptionCustomization(subscriptionRendererSingBox)
	singCustomization.PolicyGroups[1].ExcludePattern = `Tokyo|Mieru`
	singRendered, err := renderSingBoxSubscription(data, singCustomization)
	if err != nil {
		t.Fatalf("renderSingBoxSubscription() policy groups error = %v", err)
	}
	var singDocument struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
		Route     map[string]interface{}   `json:"route"`
	}
	if err := json.Unmarshal([]byte(singRendered), &singDocument); err != nil {
		t.Fatalf("policy-group sing-box output is not JSON: %v\n%s", err, singRendered)
	}
	auto := findExportByType(t, singDocument.Outbounds, "urltest")
	autoMembers, ok := auto["outbounds"].([]interface{})
	if !ok || len(autoMembers) != 4 || auto["tag"] != "自动选择" {
		t.Fatalf("sing-box filtered auto selection = %#v", auto)
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_SING_BOX_VALIDATE_BIN")); validator != "" {
		configPath := filepath.Join(t.TempDir(), "policy-groups-sing-box.json")
		if err := os.WriteFile(configPath, []byte(singRendered), 0o600); err != nil {
			t.Fatalf("write balanced sing-box validation fixture: %v", err)
		}
		output, err := exec.Command(validator, "check", "-c", configPath).CombinedOutput()
		if err != nil {
			t.Fatalf("balanced sing-box validation failed: %v\n%s\n%s", err, output, singRendered)
		}
	}

	zeroCustomization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	zeroRendered, err := renderZnetSinkSubscription(data, zeroCustomization)
	if err != nil {
		t.Fatalf("renderZnetSinkSubscription() policy groups error = %v", err)
	}
	var zeroDocument struct {
		OutboundGroups []map[string]interface{} `json:"outbound_groups"`
	}
	if err := json.Unmarshal([]byte(zeroRendered), &zeroDocument); err != nil {
		t.Fatalf("policy-group Zero output is not JSON: %v\n%s", err, zeroRendered)
	}
	if len(zeroDocument.OutboundGroups) != 2 || zeroDocument.OutboundGroups[1]["type"] != "url_test" {
		t.Fatalf("Zero policy groups = %#v", zeroDocument.OutboundGroups)
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN")); validator != "" {
		configPath := filepath.Join(t.TempDir(), "balanced-znet-sink-subscription.json")
		if err := os.WriteFile(configPath, []byte(zeroRendered), 0o600); err != nil {
			t.Fatalf("write balanced Zero validation fixture: %v", err)
		}
		output, err := exec.Command(validator, "validate", configPath).CombinedOutput()
		if err != nil {
			t.Fatalf("balanced zero validate failed: %v\n%s\n%s", err, output, zeroRendered)
		}
	}
}

func TestSubscriptionExportersRejectMissingRequiredCredential(t *testing.T) {
	data := sampleSubscriptionTemplateData()
	delete(data.ProtocolEndpoints[0].Config, "id")
	if _, err := renderClashSubscription(data, defaultSubscriptionCustomization(subscriptionRendererClash)); err == nil || !strings.Contains(err.Error(), "missing id or uuid") {
		t.Fatalf("renderClashSubscription() error = %v, want missing credential", err)
	}
	if _, err := renderSingBoxSubscription(data, defaultSubscriptionCustomization(subscriptionRendererSingBox)); err == nil || !strings.Contains(err.Error(), "missing id or uuid") {
		t.Fatalf("renderSingBoxSubscription() error = %v, want missing credential", err)
	}
}

func TestSubscriptionCustomizationAddsRuleSetsAndAdvancedOverlay(t *testing.T) {
	raw := json.RawMessage(`{
		"version": 1,
		"group_name": "Premium",
		"final": "direct",
		"rule_sets": [{
			"tag": "ads",
			"url": "https://example.com/ads.yaml",
			"behavior": "domain",
			"format": "yaml",
			"action": "reject",
			"interval": 3600
		}],
		"advanced_source": "dns:\n  enable: true"
	}`)
	customization, _, err := normalizeSubscriptionCustomization(subscriptionRendererClash, raw)
	if err != nil {
		t.Fatalf("normalizeSubscriptionCustomization() error = %v", err)
	}
	rendered, err := renderClashSubscription(subscriptionExporterTestData(), customization)
	if err != nil {
		t.Fatalf("renderClashSubscription() error = %v", err)
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("customized Clash output is not YAML: %v", err)
	}
	if !strings.Contains(rendered, "RULE-SET,ads,REJECT") || !strings.Contains(rendered, "MATCH,DIRECT") {
		t.Fatalf("customized Clash rules missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "rule-providers:") || !strings.Contains(rendered, "dns:") {
		t.Fatalf("customized Clash sections missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "name: Premium") {
		t.Fatalf("customized Clash group missing:\n%s", rendered)
	}
}

func TestSubscriptionCustomizationTranslatesRuleSetPerRenderer(t *testing.T) {
	tests := []struct {
		renderer string
		raw      string
		wants    []string
	}{
		{
			renderer: subscriptionRendererZnetSink,
			raw:      `{"version":1,"rule_sets":[{"tag":"ads","url":"https://example.com/ads.zrs","format":"zrs","action":"reject","interval":3600}]}`,
			wants:    []string{`"type": "url"`, `"format": "zrs"`, `"type": "reject"`},
		},
		{
			renderer: subscriptionRendererSingBox,
			raw:      `{"version":1,"rule_sets":[{"tag":"ads","url":"https://example.com/ads.srs","format":"binary","action":"direct","interval":3600}]}`,
			wants:    []string{`"type": "remote"`, `"format": "binary"`, `"rule_set"`, `"outbound": "direct"`},
		},
	}
	for _, test := range tests {
		t.Run(test.renderer, func(t *testing.T) {
			rendered, _, err := renderSubscriptionWithRenderer(test.renderer, json.RawMessage(test.raw), sampleSubscriptionTemplateData())
			if err != nil {
				t.Fatalf("renderSubscriptionWithRenderer() error = %v", err)
			}
			for _, want := range test.wants {
				if !strings.Contains(rendered, want) {
					t.Fatalf("rendered output missing %q:\n%s", want, rendered)
				}
			}
		})
	}
}

func TestAdvancedConfigurationExpandsManagedNodeInjectionAndRejectsBrokenReferences(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererClash)
	customization.AdvancedSource = `
proxies:
  - $zboard:generated-proxies
proxy-groups:
  - name: Advanced
    type: select
    proxies:
      - $zboard:all-nodes
rules:
  - MATCH,Advanced
`
	rendered, err := renderClashSubscription(subscriptionExporterTestData(), customization)
	if err != nil {
		t.Fatalf("renderClashSubscription() advanced injection error = %v", err)
	}
	if strings.Contains(rendered, "$zboard:") {
		t.Fatalf("advanced injection marker leaked into output:\n%s", rendered)
	}
	var document clashSubscriptionDocument
	if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("advanced Clash output is not YAML: %v", err)
	}
	if len(document.Proxies) != 6 || len(document.ProxyGroups) != 1 || len(document.ProxyGroups[0].Proxies) != 6 {
		t.Fatalf("advanced injected document = %#v", document)
	}

	customization.AdvancedSource = `
proxy-groups:
  - name: Broken
    type: select
    proxies: [missing-node]
rules:
  - MATCH,Broken
`
	if _, err := renderClashSubscription(subscriptionExporterTestData(), customization); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("renderClashSubscription() broken reference error = %v", err)
	}

	customization.AdvancedSource = `
proxy-groups:
  - name: Group A
    type: select
    proxies: [Group B]
  - name: Group B
    type: select
    proxies: [Group A]
rules:
  - MATCH,Group A
`
	if _, err := renderClashSubscription(subscriptionExporterTestData(), customization); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("renderClashSubscription() cyclic groups error = %v", err)
	}

	singCustomization := defaultSubscriptionCustomization(subscriptionRendererSingBox)
	singCustomization.AdvancedSource = `{"outbounds":[{"$zboard":"generated-outbounds"}],"route":{"final":"missing"}}`
	if _, err := renderSingBoxSubscription(subscriptionExporterTestData(), singCustomization); err == nil || !strings.Contains(err.Error(), "route.final") {
		t.Fatalf("renderSingBoxSubscription() broken final error = %v", err)
	}
}

func findExportByType(t *testing.T, items []map[string]interface{}, protocol string) map[string]interface{} {
	t.Helper()
	for _, item := range items {
		if item["type"] == protocol {
			return item
		}
	}
	t.Fatalf("export does not contain protocol %q: %#v", protocol, items)
	return nil
}

func subscriptionExporterTestData() subscriptionTemplateData {
	return subscriptionTemplateData{
		SiteName:    "Zboard",
		Version:     "zboard.subscription/v1",
		GeneratedAt: "2026-07-24T00:00:00Z",
		Subscription: subscriptionManifestSummary{
			ExpiresAt: "2026-08-24T00:00:00Z",
		},
		ProtocolEndpoints: []subscriptionTemplateEndpoint{
			{
				ID: 1, NodeID: 1, SubscriptionID: 10, Name: "Hong Kong VLESS", Address: "vless.example.com",
				Port: 443, PublicPort: 443, Protocol: "vless",
				Config: map[string]interface{}{
					"type": "vless", "id": "11111111-1111-4111-8111-111111111111",
					"reality": map[string]interface{}{"public_key": "9AwHi13y1rN6EWTSo8-HNCOhrzr251jNY7SSIxo0diA", "short_id": "0123456789abcdef", "server_name": "edge.example.com", "client_fingerprint": "firefox"},
				},
			},
			{
				ID: 2, NodeID: 2, SubscriptionID: 10, Name: "Tokyo VMess", Address: "vmess.example.com",
				Port: 443, PublicPort: 443, Protocol: "vmess",
				Config: map[string]interface{}{
					"type": "vmess", "id": "22222222-2222-4222-8222-222222222222", "cipher": "auto",
					"tls": map[string]interface{}{"server_name": "vmess.example.com"},
					"ws":  map[string]interface{}{"path": "/subscription", "headers": map[string]interface{}{"Host": "vmess.example.com"}},
				},
			},
			{
				ID: 3, NodeID: 3, Name: "Singapore Trojan", Address: "trojan.example.com",
				Port: 443, PublicPort: 443, Protocol: "trojan",
				Config: map[string]interface{}{"type": "trojan", "password": "trojan-secret", "sni": "trojan.example.com"},
			},
			{
				ID: 4, NodeID: 4, SubscriptionID: 10, Name: "Los Angeles SS", Address: "ss.example.com",
				Port: 8388, PublicPort: 18388, Protocol: "shadowsocks",
				Config: map[string]interface{}{"type": "shadowsocks", "password": "ss-secret", "cipher": "aes-128-gcm"},
			},
			{
				ID: 5, NodeID: 5, Name: "Frankfurt Hysteria2", Address: "hy2.example.com",
				Port: 443, PublicPort: 443, Protocol: "hysteria2",
				Config: map[string]interface{}{"type": "hysteria2", "password": "hy2-secret", "sni": "hy2.example.com", "insecure": false},
			},
			{
				ID: 6, NodeID: 6, Name: "Seoul Mieru", Address: "mieru.example.com",
				Port: 2999, PublicPort: 2999, Protocol: "mieru",
				Config: map[string]interface{}{"type": "mieru", "username": "subscriber", "password": "mieru-secret", "transport": "tcp"},
			},
		},
	}
}
