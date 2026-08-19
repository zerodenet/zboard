package handler

import (
	"strings"
	"testing"
)

func TestSiteCustomizationDefaultsKeepFixedProductActionsOutOfSettings(t *testing.T) {
	for _, config := range siteCustomizationDefaults() {
		if config.ConfigKey == "site_home_primary_cta" {
			t.Fatal("fixed-destination homepage CTA must not be exposed as operator configuration")
		}
	}
}

func TestPolicyDefaultsUseDynamicSiteContact(t *testing.T) {
	for name, content := range map[string]string{
		"terms":   defaultTermsContent,
		"privacy": defaultPrivacyContent,
		"refund":  defaultRefundContent,
	} {
		if !strings.Contains(content, "{{support_contact}}") {
			t.Fatalf("%s policy template must use support_contact variable", name)
		}
		if !strings.Contains(content, "{{copyright}}") {
			t.Fatalf("%s policy template must include dynamic copyright", name)
		}
	}
}

func TestLegacyPolicyKeysMapToNewContentKeys(t *testing.T) {
	want := map[string]string{
		"site_terms_content":   "site_terms_url",
		"site_privacy_content": "site_privacy_url",
		"site_refund_content":  "site_refund_url",
	}
	for contentKey, legacyKey := range want {
		if got := legacyPolicyKeys[contentKey]; got != legacyKey {
			t.Fatalf("legacy mapping for %s = %q, want %q", contentKey, got, legacyKey)
		}
	}
}
