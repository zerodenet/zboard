package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestZeroRuleIRNormalizesAndCanonicalizes(t *testing.T) {
	raw := []byte(`{
  "version": 1,
  "name": "sample",
  "rules": [
    {"type":"domain_exact","value":"Ads.Example.COM."},
    {"type":"domain_suffix","value":"example.com"},
    {"type":"domain_suffix","value":"sub.example.com"},
    {"type":"domain_exact","value":"BÜCHER.example"},
    {"type":"domain_keyword","value":" ADS "},
    {"type":"domain_keyword","value":"ads"},
    {"type":"ipv4_cidr","value":"192.0.2.7/24"},
    {"type":"ipv6_cidr","value":"2001:db8::1/32"}
  ]
}`)
	document, err := parseManagedRuleSource(raw, managedRuleSourceZeroRuleIR)
	if err != nil {
		t.Fatalf("parseManagedRuleSource() error = %v", err)
	}
	want := []managedRule{
		{Type: managedRuleTypeDomainExact, Value: "xn--bcher-kva.example"},
		{Type: managedRuleTypeDomainSuffix, Value: "example.com"},
		{Type: managedRuleTypeDomainKeyword, Value: "ads"},
		{Type: managedRuleTypeIPv4CIDR, Value: "192.0.2.0/24"},
		{Type: managedRuleTypeIPv6CIDR, Value: "2001:db8::/32"},
	}
	if len(document.Rules) != len(want) {
		t.Fatalf("rules = %#v, want %#v", document.Rules, want)
	}
	for index := range want {
		if document.Rules[index] != want[index] {
			t.Fatalf("rules[%d] = %#v, want %#v", index, document.Rules[index], want[index])
		}
	}
	canonical := encodeManagedCanonicalSource(document)
	var roundTrip managedRuleDocument
	if err := json.Unmarshal(canonical, &roundTrip); err != nil {
		t.Fatalf("canonical JSON error = %v", err)
	}
	if !strings.HasSuffix(string(canonical), "\n") || roundTrip.Version != 1 {
		t.Fatalf("canonical = %s", canonical)
	}
}

func TestZeroRuleIRRejectsStrictProtocolViolations(t *testing.T) {
	tests := []string{
		`{"version":2,"rules":[{"type":"domain_exact","value":"example.com"}]}`,
		`{"version":1,"extra":true,"rules":[{"type":"domain_exact","value":"example.com"}]}`,
		`{"version":1,"rules":[{"type":"domain_exact","value":"example.com","extra":true}]}`,
		`{"version":1,"rules":null}`,
		`{"version":1,"rules":[]}`,
		`{"version":1,"rules":[{"type":"domain_regex","value":"example"}]}`,
		`{"version":1,"rules":[{"type":"domain_keyword","value":"广告"}]}`,
		`{"version":1,"rules":[{"type":"ipv4_cidr","value":"2001:db8::/32"}]}`,
	}
	for _, raw := range tests {
		if _, err := parseManagedRuleSource([]byte(raw), managedRuleSourceZeroRuleIR); err == nil {
			t.Fatalf("parseManagedRuleSource(%s) succeeded, want error", raw)
		}
	}
}

func TestManagedRuleImportsConvertToZeroRuleIR(t *testing.T) {
	tests := []struct {
		format string
		raw    string
		typeID string
		value  string
	}{
		{managedRuleSourceDomainList, "domain:Example.COM.\n", managedRuleTypeDomainSuffix, "example.com"},
		{managedRuleSourceCIDRList, "192.0.2.7/24\n", managedRuleTypeIPv4CIDR, "192.0.2.0/24"},
		{managedRuleSourceClashClassical, "DOMAIN-KEYWORD,Ads,REJECT\n", managedRuleTypeDomainKeyword, "ads"},
	}
	for _, test := range tests {
		document, err := parseManagedRuleSource([]byte(test.raw), test.format)
		if err != nil {
			t.Fatalf("parse %s: %v", test.format, err)
		}
		if len(document.Rules) != 1 || document.Rules[0].Type != test.typeID || document.Rules[0].Value != test.value {
			t.Fatalf("parse %s = %#v", test.format, document.Rules)
		}
	}
}

func TestManagedRuleDerivedArtifacts(t *testing.T) {
	document := managedRuleDocument{Version: 1, Rules: []managedRule{
		{Type: managedRuleTypeDomainSuffix, Value: "example.com"},
		{Type: managedRuleTypeIPv4CIDR, Value: "192.0.2.0/24"},
	}}
	clash := string(encodeManagedRuleClashYAML(document))
	if !strings.Contains(clash, "DOMAIN-SUFFIX,example.com") || !strings.Contains(clash, "IP-CIDR,192.0.2.0/24") {
		t.Fatalf("clash = %s", clash)
	}
	singBox, err := encodeManagedRuleSingBox(document)
	if err != nil {
		t.Fatalf("encodeManagedRuleSingBox() error = %v", err)
	}
	if !strings.Contains(string(singBox), `"domain_suffix"`) || !strings.Contains(string(singBox), `"ip_cidr"`) {
		t.Fatalf("sing-box = %s", singBox)
	}
	if got := managedRuleFormatFromUserAgent("znet-sink/1.0"); got != managedRuleArtifactZRS {
		t.Fatalf("znet-sink format = %q", got)
	}
}
