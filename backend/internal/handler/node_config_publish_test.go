package handler

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOptionalRequesterIDTreatsSystemPublishAsNull(t *testing.T) {
	if requester := optionalRequesterID(0); requester != nil {
		t.Fatalf("system requester = %v, want nil", *requester)
	}

	requester := optionalRequesterID(7)
	if requester == nil || *requester != 7 {
		t.Fatalf("admin requester = %v, want 7", requester)
	}
}

func TestFinalizeConnectorConfirmationUsesConnectorEventTime(t *testing.T) {
	confirmedAt := time.Date(2026, time.August, 5, 1, 25, 43, 0, time.UTC)
	fallback := confirmedAt.Add(time.Minute)

	output, healthyAt := finalizeConnectorConfirmation("ZBOARD_CONFIG_APPLIED=abc123\n", confirmedAt, nil, fallback)

	if output != "ZBOARD_CONFIG_APPLIED=abc123" {
		t.Fatalf("unexpected output: %q", output)
	}
	if !healthyAt.Equal(confirmedAt) {
		t.Fatalf("expected connector event time %s, got %s", confirmedAt, healthyAt)
	}
}

func TestFinalizeConnectorConfirmationRecordsPendingWarningWithoutRollback(t *testing.T) {
	fallback := time.Date(2026, time.August, 5, 1, 26, 37, 0, time.UTC)
	connectorErr := errors.New("no fresh connector event arrived within 55s:\ncontext deadline exceeded")

	output, healthyAt := finalizeConnectorConfirmation("ZBOARD_CONFIG_APPLIED=abc123", time.Time{}, connectorErr, fallback)

	if !strings.Contains(output, "ZBOARD_CONFIG_APPLIED=abc123") {
		t.Fatalf("applied marker missing from output: %q", output)
	}
	if !strings.Contains(output, "ZBOARD_CONNECTOR_CONFIRMATION_PENDING=no fresh connector event arrived within 55s: context deadline exceeded") {
		t.Fatalf("connector warning missing from output: %q", output)
	}
	if strings.Contains(output, "ZBOARD_CONFIG_ROLLED_BACK") {
		t.Fatalf("connector timeout must not report a rollback: %q", output)
	}
	if !healthyAt.Equal(fallback) {
		t.Fatalf("expected direct health-check time %s, got %s", fallback, healthyAt)
	}
}
