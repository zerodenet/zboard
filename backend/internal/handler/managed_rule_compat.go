package handler

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Compatibility aliases keep the in-progress managed rule-set branch readable
// while the persisted canonical format is now Zero Rule IR v1.
const (
	managedRuleSourceCanonical = managedRuleSourceZeroRuleIR
	managedRuleArtifactZRS     = managedRuleArtifactZeroRuleIR
)

// MarshalJSON rewrites only Zboard-owned rule URLs that were produced through
// the previous zrs-shaped template path. Legacy external ZRS providers keep
// their original format value.
func (value subscriptionRuleSetCustomization) MarshalJSON() ([]byte, error) {
	type wire subscriptionRuleSetCustomization
	if value.Format == "zrs" && managedRuleURLUsesZeroRuleIR(value.URL) {
		value.Format = managedRuleSourceZeroRuleIR
	}
	return json.Marshal(wire(value))
}

func managedRuleURLUsesZeroRuleIR(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	format, err := normalizeManagedRuleArtifactFormat(parsed.Query().Get("format"))
	return err == nil && format == managedRuleArtifactZeroRuleIR
}
