package handler

import (
	"strings"
	"testing"
)

func TestManagedRuleAutoDetectsFormatsAndPreservesExplicitSelection(t *testing.T) {
	for _, raw := range []string{
		"\xef\xbb\xbfpayload:\n - DOMAIN-SUFFIX,example.com\n - PROCESS-NAME,App.exe\n",
		"# comment\n192.0.2.0/24\n",
		"# comment\nexample.com\n",
		`{"version":1,"rules":[{"type":"domain_exact","value":"example.com"}]}`,
	} {
		if _, err := parseManagedRuleSource([]byte(raw), managedRuleSourceAuto); err != nil {
			t.Fatalf("auto detect %q: %v", raw, err)
		}
	}
	if _, err := parseManagedRuleSource([]byte("DOMAIN-SUFFIX,example.com"), managedRuleSourceCIDRList); err == nil || !strings.Contains(err.Error(), "Clash classical") {
		t.Fatalf("explicit format silently changed: %v", err)
	}
	for _, raw := range []string{`{"route":{"rules":[]}}`, `"route": {`, "# comments only"} {
		if _, err := parseManagedRuleSource([]byte(raw), managedRuleSourceAuto); err == nil {
			t.Fatalf("accepted configuration or empty source: %q", raw)
		}
	}
}
