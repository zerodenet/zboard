package handler

import (
	"encoding/json"
	"net/url"
	"strings"
)

func (ruleSet subscriptionRuleSetCustomization) MarshalJSON() ([]byte, error) {
	type ruleSetAlias subscriptionRuleSetCustomization
	normalized := ruleSetAlias(ruleSet)
	if rewritten, ok := managedRuleLegacyIRURLToZRS(normalized.URL, normalized.Format); ok {
		normalized.URL = rewritten
		normalized.Format = managedRuleArtifactZRS
	}
	return json.Marshal(normalized)
}

func managedRuleLegacyIRURLToZRS(rawURL, format string) (string, bool) {
	if strings.ToLower(strings.TrimSpace(format)) != managedRuleSourceZeroRuleIR {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.HasPrefix(parsed.Path, "/api/v1/rules/") {
		return "", false
	}
	queryFormat := strings.ToLower(strings.TrimSpace(parsed.Query().Get("format")))
	if queryFormat != managedRuleSourceZeroRuleIR {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Path = strings.TrimSuffix(parsed.Path, ".zrs") + ".zrs"
	return parsed.String(), true
}
