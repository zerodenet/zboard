package model

import (
	"strings"
	"testing"
)

func TestPolicyContentLimit(t *testing.T) {
	config := &SystemConfig{ConfigKey: "site_terms_content", Value: strings.Repeat("a", maxSitePolicyContentBytes)}
	if err := config.BeforeSave(nil); err != nil {
		t.Fatalf("content at limit should be accepted: %v", err)
	}
	config.Value += "a"
	if err := config.BeforeSave(nil); err == nil {
		t.Fatal("content above limit should be rejected")
	}
}

func TestSitePublicURLValidation(t *testing.T) {
	for _, key := range []string{"site_logo_dark", "site_favicon", "site_support_url", "site_telegram_url"} {
		if err := validateSiteSystemConfig(key, "https://example.com/value"); err != nil {
			t.Fatalf("%s valid URL rejected: %v", key, err)
		}
		if err := validateSiteSystemConfig(key, "/relative/path"); err == nil {
			t.Fatalf("%s relative URL should be rejected", key)
		}
		if err := validateSiteSystemConfig(key, "javascript:alert(1)"); err == nil {
			t.Fatalf("%s unsafe URL should be rejected", key)
		}
	}
}

func TestSiteSupportEmailValidation(t *testing.T) {
	if err := validateSiteSystemConfig("site_support_email", "support@example.com"); err != nil {
		t.Fatalf("valid support email rejected: %v", err)
	}
	if err := validateSiteSystemConfig("site_support_email", "Support <support@example.com>"); err == nil {
		t.Fatal("display-name email should not be accepted as a raw support address")
	}
}

func TestSiteLegalItemsValidation(t *testing.T) {
	valid := `[{"label":"Company No.","value":"12345678","url":"https://registry.example/12345678"}]`
	if err := validateSiteSystemConfig("site_legal_items", valid); err != nil {
		t.Fatalf("valid legal items rejected: %v", err)
	}
	if err := validateSiteSystemConfig("site_legal_items", `[{"label":"","value":"123"}]`); err == nil {
		t.Fatal("empty label should be rejected")
	}
	if err := validateSiteSystemConfig("site_legal_items", `[{"label":"VAT","value":"123","url":"/relative"}]`); err == nil {
		t.Fatal("relative registry URL should be rejected")
	}
}

func TestSitePolicyDocumentsValidation(t *testing.T) {
	valid := `[{"slug":"fair-use","title":"公平使用政策","summary":"适用限制","content":"# 公平使用政策\n\n正文","published":true,"placements":["footer","purchase"]}]`
	if err := validateSiteSystemConfig("site_policy_documents", valid); err != nil {
		t.Fatalf("valid policy documents rejected: %v", err)
	}
	if err := validateSiteSystemConfig("site_policy_documents", "null"); err != nil {
		t.Fatalf("legacy null sentinel rejected: %v", err)
	}
	for name, value := range map[string]string{
		"duplicate slug": `[{"slug":"terms","title":"A","content":"A"},{"slug":"terms","title":"B","content":"B"}]`,
		"invalid slug":   `[{"slug":"Fair Use","title":"A","content":"A"}]`,
		"empty content":  `[{"slug":"terms","title":"A","content":""}]`,
		"bad placement":  `[{"slug":"terms","title":"A","content":"A","placements":["checkout"]}]`,
	} {
		if err := validateSiteSystemConfig("site_policy_documents", value); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}
}
