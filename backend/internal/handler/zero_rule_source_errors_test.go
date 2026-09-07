package handler

import (
	"strings"
	"testing"
)

func TestClashAdBlockProviderRequiresClassicalSourceFormat(t *testing.T) {
	raw := []byte(`# Ads in Video apps
payload:
  - DOMAIN-SUFFIX,a.ckm.iqiyi.com
  - DOMAIN-SUFFIX,09_19.supfree.net
  - DOMAIN,ads.example.com
  - DOMAIN-KEYWORD,advert
  - IP-CIDR,101.227.97.240/32,no-resolve
  - IP-CIDR6,2001:db8::/32,no-resolve
`)
	for _, format := range []string{managedRuleSourceCIDRList, managedRuleSourceDomainList} {
		_, err := parseManagedRuleSource(raw, format)
		if err == nil || !strings.Contains(err.Error(), "payload 第 1 项") || !strings.Contains(err.Error(), "DOMAIN-SUFFIX") || !strings.Contains(err.Error(), "改为“Clash classical”") {
			t.Fatalf("format %s: %v", format, err)
		}
	}
	document, err := parseManagedRuleSource(raw, managedRuleSourceClashClassical)
	if err != nil || len(document.Rules) != 6 {
		t.Fatalf("classical import: rules=%d error=%v", len(document.Rules), err)
	}
}

func TestRuleDomainsAllowUnderscoresWithoutAcceptingRuleSyntax(t *testing.T) {
	for _, ruleType := range []string{managedRuleTypeDomainExact, managedRuleTypeDomainSuffix} {
		for raw, want := range map[string]string{
			"09_19.SUPFREE.NET.":      "09_19.supfree.net",
			"_service.BÜCHER.example": "_service.xn--bcher-kva.example",
		} {
			got, err := normalizeManagedRuleValue(ruleType, raw)
			if err != nil || got != want {
				t.Fatalf("normalize %q = %q, %v", raw, got, err)
			}
		}
		for _, raw := range []string{"ads_1.example/path", "ads_1.example,REJECT", "ads_1 example", "*.example.com", "ads_1..example", "ads_1.example:443", "ads_1@example.com", "ads_1\x00.example", strings.Repeat("a", 64) + ".example"} {
			if _, err := normalizeManagedRuleValue(ruleType, raw); err == nil {
				t.Fatalf("accepted malformed rule domain %q", raw)
			}
		}
	}
}

func TestCIDRSourceFailureExplainsExpectedSyntax(t *testing.T) {
	for _, raw := range []string{"# comment\nexample.com\n", "192.0.2.0/999\n"} {
		_, err := parseManagedRuleSource([]byte(raw), managedRuleSourceCIDRList)
		if err == nil || !strings.Contains(err.Error(), "只接受纯 IP 网段") || !strings.Contains(err.Error(), "192.0.2.0/24") {
			t.Fatalf("CIDR error=%v", err)
		}
	}
	if _, err := parseManagedRuleSource([]byte("192.0.2.0/24\n"), managedRuleSourceClashClassical); err == nil || !strings.Contains(err.Error(), "请选择“CIDR 列表”") {
		t.Fatalf("classical error=%v", err)
	}
}
