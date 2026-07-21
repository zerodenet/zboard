package api

import (
	"bytes"
	"os"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestOpenAPIIsValidYAMLAndContainsCoreCommercialPaths(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[interface{}]interface{}
	if err := yaml.UnmarshalStrict(payload, &document); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	paths, ok := document["paths"].(map[interface{}]interface{})
	if !ok {
		t.Fatal("OpenAPI paths object is missing")
	}
	for _, path := range []string{
		"/api/v1/setup/install",
		"/api/v1/admin/protocol-endpoints",
		"/api/v1/admin/protocol-endpoints/{id}",
		"/api/v1/admin/protocol-endpoints/{id}/deploy",
		"/api/v1/admin/protocol-deployments",
		"/api/v1/nodes/{id}",
		"/api/v1/nodes/{id}/ssh",
		"/api/v1/nodes/{id}/ssh/host-key/reset",
		"/api/v1/nodes/{id}/ssh/terminal-ticket",
		"/api/v1/nodes/{id}/ssh/terminal",
		"/api/v1/nodes/{id}/kernel",
		"/api/v1/nodes/{id}/kernel/detect",
		"/api/v1/nodes/{id}/kernel/reconcile",
		"/api/v1/admin/kernel/releases/latest",
		"/api/v1/nodes/{id}/connector-credential",
		"/api/v1/nodes/{id}/heartbeat",
		"/api/v1/nodes/{id}/commands",
		"/api/v1/admin/plans/{id}/skus",
		"/api/v1/admin/node-groups",
		"/api/v1/admin/system-configs/{key}",
		"/api/v1/admin/tasks",
		"/api/v1/admin/tasks/{id}/run",
		"/api/v1/tickets",
		"/api/v1/tickets/{id}/messages",
		"/api/v1/admin/tickets",
		"/api/v1/admin/tickets/{id}/messages",
		"/api/v1/admin/tickets/{id}/status",
		"/api/v1/orders",
		"/api/v1/admin/orders",
		"/api/v1/admin/subscriptions",
		"/api/v1/admin/traffic/records",
		"/api/v1/traffic/report",
	} {
		if _, ok := paths[path]; !ok {
			t.Errorf("OpenAPI path %s is missing", path)
		}
	}
	for _, removed := range []string{
		"/api/v1/protocol-endpoints",
		"/api/v1/nodes/protocol/config",
		"/api/v1/admin/access-groups",
		"/api/v1/admin/plans/{id}/protocol-endpoints",
	} {
		if _, ok := paths[removed]; ok {
			t.Errorf("removed OpenAPI path %s is still present", removed)
		}
	}
	for _, removedField := range [][]byte{
		[]byte("access_group_id:"),
		[]byte("traffic_multiplier_milli:"),
		[]byte("subscription_multiplier_milli:"),
		[]byte("ssh_host_key_policy:"),
		[]byte("account_type:"),
	} {
		if bytes.Contains(payload, removedField) {
			t.Errorf("removed OpenAPI field %s is still present", removedField)
		}
	}
}
