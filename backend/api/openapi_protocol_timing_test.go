package api

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAPIDocumentsProtocolEndpointMutationTiming(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	document := string(payload)
	for _, expected := range []string{
		"ProtocolEndpointMutationTiming:",
		"validation_ms:",
		"transaction_ms:",
		"task_enqueue_ms:",
		"response_preparation_ms:",
		"server_total_ms:",
		`timing: { $ref: "#/components/schemas/ProtocolEndpointMutationTiming" }`,
	} {
		if !strings.Contains(document, expected) {
			t.Fatalf("OpenAPI protocol timing contract omits %q", expected)
		}
	}
}
