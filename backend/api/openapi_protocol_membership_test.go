package api

import (
	"os"
	"strings"
	"testing"
)

func TestProtocolEndpointAdminDetailUsesDocumentedDeploymentSummary(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	document := string(payload)
	if strings.Contains(document, `#/components/schemas/ProtocolDeployment"`) {
		t.Fatal("protocol endpoint admin detail references an undefined ProtocolDeployment schema")
	}
	if !strings.Contains(document, `latest_deployment: { $ref: "#/components/schemas/ProtocolDeploymentSummary" }`) {
		t.Fatal("protocol endpoint admin detail must reference ProtocolDeploymentSummary")
	}
	if !strings.Contains(document, "ProtocolDeploymentSummary:") {
		t.Fatal("ProtocolDeploymentSummary schema is missing")
	}
}
