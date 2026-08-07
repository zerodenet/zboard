package handler

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Keep the renderer spelling used by the in-progress managed-rule implementation
// aligned with the canonical renderer constant already used by subscription code.
const subscriptionRendererZNetSink = subscriptionRendererZnetSink

// Legacy internal names remain aliases only; the public artifact and the client
// configuration both identify this payload as Zero Rule IR v1, never as ZRS.
const (
	managedRuleSourceCanonical = managedRuleSourceZeroRuleIR
	managedRuleArtifactZRS     = managedRuleArtifactZeroRuleIR
)

// MarshalJSON rewrites the temporary zrs-shaped internal value used while the
// branch was being developed. External providers are not rewritten.
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
