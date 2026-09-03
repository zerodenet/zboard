package api

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestOpenAPITrafficStatisticsParametersKeepDescriptionsIntact(t *testing.T) {
	payload, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[interface{}]interface{}
	if err := yaml.UnmarshalStrict(payload, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[interface{}]interface{})
	for _, path := range []string{"/api/v1/traffic/records", "/api/v1/admin/traffic/records"} {
		t.Run(path, func(t *testing.T) {
			operation := paths[path].(map[interface{}]interface{})["get"].(map[interface{}]interface{})
			found := false
			for _, raw := range operation["parameters"].([]interface{}) {
				parameter := raw.(map[interface{}]interface{})
				if parameter["name"] != "include_totals" {
					continue
				}
				found = true
				// An unquoted comma in a YAML flow mapping silently creates a key.
				if len(parameter) != 4 || parameter["in"] != "query" {
					t.Fatalf("unexpected parameter fields: %#v", parameter)
				}
				description, _ := parameter["description"].(string)
				if !strings.Contains(description, "page.total, total and aggregates are null.") ||
					!strings.Contains(description, "view=usage_summary") {
					t.Fatalf("truncated statistics description: %q", description)
				}
				schema := parameter["schema"].(map[interface{}]interface{})
				if schema["type"] != "boolean" || schema["default"] != true {
					t.Fatalf("legacy totals must remain enabled by default: %#v", schema)
				}
			}
			if !found {
				t.Fatal("include_totals parameter missing")
			}
		})
	}
}
