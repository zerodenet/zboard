package handler

import (
	"strings"
	"testing"
)

func TestValidateEndpointCredentialProjectionAllowsEmptyAudience(t *testing.T) {
	if err := validateEndpointCredentialProjectionCount(7, 0, 0); err != nil {
		t.Fatalf("endpoint without subscriptions must remain publishable: %v", err)
	}
}

func TestValidateEndpointCredentialProjectionRejectsMissingCredentialsForActiveSubscriptions(t *testing.T) {
	err := validateEndpointCredentialProjectionCount(7, 2, 0)
	if err == nil || !strings.Contains(err.Error(), "active subscriptions but no active credentials") {
		t.Fatalf("error = %v, want credential projection inconsistency", err)
	}
}

func TestValidateEndpointCredentialProjectionAllowsMaterializedCredentials(t *testing.T) {
	if err := validateEndpointCredentialProjectionCount(7, 2, 2); err != nil {
		t.Fatalf("materialized credentials must pass: %v", err)
	}
}
