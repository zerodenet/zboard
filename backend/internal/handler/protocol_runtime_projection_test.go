package handler

import (
	"os"
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

func TestRuntimeEndpointDoesNotDisappearWhenCredentialSetIsEmpty(t *testing.T) {
	payload, err := os.ReadFile("protocol_credentials.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	legacySilentDrop := "if len(credentials) == 0 {\n\t\treturn nil, nil\n\t}"
	if strings.Contains(source, legacySilentDrop) {
		t.Fatal("an active endpoint must not disappear merely because it has no active credentials")
	}
	if !strings.Contains(source, "h.validateEndpointCredentialProjection(endpoint.ID, now)") {
		t.Fatal("runtime endpoint compilation must validate credential projection before materializing an empty-user listener")
	}
}
