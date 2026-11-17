package api

import (
	"gopkg.in/yaml.v2"
	"os"
	"testing"
)

func TestObservationIntentOpenAPIIsReadOnlyBoundedMetadata(t *testing.T) {
	raw, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[interface{}]interface{}
	if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[interface{}]interface{})
	path := paths["/api/v1/admin/node-observation-intents"].(map[interface{}]interface{})
	if len(path) != 1 || path["get"] == nil {
		t.Fatal("queue API gained a write operation")
	}
	get := path["get"].(map[interface{}]interface{})
	responses := get["responses"].(map[interface{}]interface{})
	badQuery := responses["400"].(map[interface{}]interface{})
	if len(badQuery) != 1 || badQuery["description"] != "Invalid status, limit, cursor or unsupported/duplicate query parameter" {
		t.Fatal("query error description became invalid response fields", badQuery)
	}
	parameters := get["parameters"].([]interface{})
	if len(parameters) != 3 {
		t.Fatal("unexpected scan/filter parameters")
	}
	seen := map[string]bool{}
	for _, raw := range parameters {
		p := raw.(map[interface{}]interface{})
		name := p["name"].(string)
		seen[name] = true
		if name == "limit" {
			schema := p["schema"].(map[interface{}]interface{})
			if schema["maximum"] != 100 || schema["minimum"] != 1 {
				t.Fatal(schema)
			}
		}
	}
	if !seen["status"] || !seen["cursor"] || !seen["limit"] {
		t.Fatal(seen)
	}
	components := doc["components"].(map[interface{}]interface{})
	schemas := components["schemas"].(map[interface{}]interface{})
	item := schemas["ObservationIntentItem"].(map[interface{}]interface{})
	properties := item["properties"].(map[interface{}]interface{})
	for _, forbidden := range []string{"token", "asset_tag", "core_instance_id", "source_id", "payload", "principal_key", "flow_id", "observed_at", "occurred_at"} {
		if properties[forbidden] != nil {
			t.Fatal("private source/sample metadata exposed", forbidden)
		}
	}
	if properties["node_id"].(map[interface{}]interface{})["type"] != "string" {
		t.Fatal("historical node ID loses uint64 precision")
	}
	for _, name := range []string{"attempts", "material_bytes", "part_count"} {
		if properties[name].(map[interface{}]interface{})["type"] != "string" {
			t.Fatal("diagnostic count loses unsigned precision", name)
		}
	}
	page := schemas["ObservationIntentPage"].(map[interface{}]interface{})["properties"].(map[interface{}]interface{})
	for name, value := range map[string]interface{}{"next_cursor": page["next_cursor"], "enqueued_at": properties["enqueued_at"], "disposition_updated_at": properties["disposition_updated_at"]} {
		field := value.(map[interface{}]interface{})
		if len(field) != 3 {
			t.Fatal("metadata schema description became invalid fields", name, field)
		}
		if _, ok := field["description"].(string); !ok {
			t.Fatal("metadata schema requires a text description", name)
		}
	}
	verified := properties["material_verified"].(map[interface{}]interface{})["enum"].([]interface{})
	if len(verified) != 1 || verified[0] != false {
		t.Fatal("metadata endpoint claims material verification")
	}
}
