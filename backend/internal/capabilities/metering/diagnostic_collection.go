package metering

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const DiagnosticCollectionKey = "traffic_report_diagnostics"
const DiagnosticCollectionSlots = 128
const DiagnosticCollectionWindow = 24 * time.Hour

// This policy concerns only optional legacy report bodies. Financial identities,
// native source proofs and cumulative cursors never use its capture budget.
type DiagnosticCollectionPolicy struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

func ParseDiagnosticCollection(value string) (*DiagnosticCollectionPolicy, error) {
	if len(value) > 1024 {
		return nil, errors.New("diagnostic collection policy exceeds1024 bytes")
	}
	if strings.TrimSpace(value) == "null" {
		return nil, nil
	}
	var policy DiagnosticCollectionPolicy
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, errors.New("diagnostic collection needs until and reason, or null to disable")
	}
	if decoder.Decode(new(any)) != io.EOF || policy.Until.IsZero() || strings.TrimSpace(policy.Reason) == "" || len(policy.Reason) > 512 {
		return nil, errors.New("diagnostic collection needs one expiry and a1–512 byte reason")
	}
	return &policy, nil
}

func ValidateDiagnosticCollection(value string, now time.Time) error {
	policy, err := ParseDiagnosticCollection(value)
	if err != nil || policy == nil {
		return err
	}
	if !policy.Until.After(now) || policy.Until.After(now.Add(DiagnosticCollectionWindow)) {
		return errors.New("diagnostic collection expiry must be in the next24 hours")
	}
	return nil
}

func (p DiagnosticCollectionPolicy) Active(now, savedAt time.Time) bool {
	return !savedAt.IsZero() && !savedAt.After(now) && p.Until.After(now) && !p.Until.After(savedAt.Add(DiagnosticCollectionWindow))
}
