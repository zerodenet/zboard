package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDecodeZeroRuleIRRejectsUnknownFields(t *testing.T) {
	tests := []string{
		`{"version":1,"rules":[{"type":"domain_exact","value":"example.com"}],"source":"remote"}`,
		`{"version":1,"rules":[{"type":"domain_exact","value":"example.com","action":"reject"}]}`,
		`{"rules":[{"type":"domain_exact","value":"example.com"}]}`,
		`{"version":2,"rules":[{"type":"domain_exact","value":"example.com"}]}`,
	}
	for _, input := range tests {
		if _, err := decodeAndNormalizeZeroRuleIR([]byte(input)); err == nil {
			t.Fatalf("expected strict Zero Rule IR validation to reject %s", input)
		}
	}
}

func TestNormalizeZeroRuleIRUsesCanonicalMatcherSemantics(t *testing.T) {
	input := `{
		"version": 1,
		"name": "Example rules",
		"rules": [
			{"type":"domain_exact","value":" API.Example.COM. "},
			{"type":"domain_suffix","value":"example.com"},
			{"type":"domain_suffix","value":"sub.example.com"},
			{"type":"domain_exact","value":"例子.测试"},
			{"type":"domain_keyword","value":" Special "},
			{"type":"ipv4_cidr","value":"10.1.2.3/8"},
			{"type":"ipv6_cidr","value":"fd00:1::1/8"}
		]
	}`
	document, err := decodeAndNormalizeZeroRuleIR([]byte(input))
	if err != nil {
		t.Fatalf("decode Zero Rule IR: %v", err)
	}

	got := make([]string, 0, len(document.Rules))
	for _, rule := range document.Rules {
		got = append(got, rule.Type+":"+rule.Value)
	}
	joined := strings.Join(got, "\n")
	for _, expected := range []string{
		"domain_exact:xn--fsqu00a.xn--0zwm56d",
		"domain_suffix:example.com",
		"domain_keyword:special",
		"ipv4_cidr:10.0.0.0/8",
		"ipv6_cidr:fd00::/8",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("normalized rules missing %q:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "api.example.com") || strings.Contains(joined, "sub.example.com") {
		t.Fatalf("suffix coverage was not eliminated:\n%s", joined)
	}
}

func TestManagedRuleTemplateUsesPlatformEndpoint(t *testing.T) {
	record := model.SubscriptionRuleSet{
		Name: "Ads",
		Tag: "reject-ads",
		Renderer: managedRuleSetRenderer,
		Interval: 3600,
	}
	resolved, err := managedRuleCustomizationForRenderer(
		subscriptionRendererZnetSink,
		"https://panel.example.com/",
		record,
		subscriptionGroupTarget(subscriptionTargetReject),
	)
	if err != nil {
		t.Fatalf("resolve managed rule set: %v", err)
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		t.Fatalf("marshal resolved rule set: %v", err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"format":"zero_rule_ir"`) {
		t.Fatalf("znet-sink must receive Zero Rule IR, got %s", text)
	}
	if !strings.Contains(text, `https://panel.example.com/api/v1/rules/reject-ads?format=zero_rule_ir`) {
		t.Fatalf("template must use the Zboard public endpoint, got %s", text)
	}
}

func TestManagedRuleArtifactNegotiation(t *testing.T) {
	cases := map[string]string{
		"znet-sink/0.0.16": managedRuleArtifactZeroRuleIR,
		"Clash.Meta/1.19": managedRuleArtifactClashClassicalYAML,
		"sing-box 1.12": managedRuleArtifactSingBoxSource,
		"curl/8.0": managedRuleArtifactZeroRuleIR,
	}
	for userAgent, expected := range cases {
		if actual := managedRuleFormatFromUserAgent(userAgent); actual != expected {
			t.Fatalf("UA %q: expected %q, got %q", userAgent, expected, actual)
		}
	}
}

func TestManagedRuleEncoders(t *testing.T) {
	document := managedRuleDocument{Version: 1, Rules: []managedRule{
		{Type: managedRuleTypeDomainExact, Value: "api.example.com"},
		{Type: managedRuleTypeDomainSuffix, Value: "example.org"},
		{Type: managedRuleTypeIPv4CIDR, Value: "10.0.0.0/8"},
	}}
	clash := string(encodeManagedRuleClashYAML(document))
	if !strings.Contains(clash, "DOMAIN,api.example.com") || !strings.Contains(clash, "IP-CIDR,10.0.0.0/8") {
		t.Fatalf("unexpected Clash artifact:\n%s", clash)
	}
	singBox, err := encodeManagedRuleSingBox(document)
	if err != nil {
		t.Fatalf("encode sing-box: %v", err)
	}
	if !strings.Contains(string(singBox), `"domain_suffix"`) || !strings.Contains(string(singBox), `"ip_cidr"`) {
		t.Fatalf("unexpected sing-box artifact:\n%s", singBox)
	}
}
