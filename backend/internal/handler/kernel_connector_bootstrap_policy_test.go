package handler

import (
	"os"
	"strings"
	"testing"
)

func TestConnectorCredentialBootstrapIsVisibleAndTransactional(t *testing.T) {
	payload, err := os.ReadFile("../capabilities/network/kernel_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	for _, expected := range []string{
		`setPhase("preparing_connector_credential")`,
		"s.State.ActivateConnectorCredential",
		"s.State.RestoreConnectorCredential",
		"the generated connector credential was rolled back because Zero activation did not complete",
		"the activated generation and generated connector credential were rolled back",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("connector bootstrap must expose transactional credential behavior: missing %q", expected)
		}
	}
}
