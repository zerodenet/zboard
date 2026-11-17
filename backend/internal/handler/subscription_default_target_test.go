package handler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestSubscriptionDefaultTargetsRenderIncludedMembers(t *testing.T) {
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererClash, subscriptionRendererSingBox} {
		for _, target := range []string{"direct", "reject", "group:auto"} {
			t.Run(renderer+"/"+target, func(t *testing.T) {
				custom := defaultSubscriptionCustomization(renderer)
				custom.PolicyGroups[0].DefaultGroup = ""
				custom.PolicyGroups[0].DefaultTarget = target
				raw, _ := json.Marshal(custom)
				custom, _, err := normalizeSubscriptionCustomization(renderer, raw)
				if renderer == subscriptionRendererSingBox && target == "reject" {
					if err == nil {
						t.Fatal("sing-box accepted a reject selector member")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				direct, reject := subscriptionSelectionTargets(renderer)
				want := map[string]string{"direct": direct, "reject": reject, "group:auto": custom.PolicyGroups[1].Name}[target]
				data := subscriptionExporterTestData()
				if renderer == subscriptionRendererClash {
					output, err := renderClashSubscription(data, custom)
					if err != nil {
						t.Fatal(err)
					}
					var doc struct {
						Groups []struct {
							Name    string   `yaml:"name"`
							Proxies []string `yaml:"proxies"`
						} `yaml:"proxy-groups"`
					}
					if err := yaml.Unmarshal([]byte(output), &doc); err != nil {
						t.Fatal(err)
					}
					if doc.Groups[0].Proxies[0] != want {
						t.Fatalf("Clash initial member = %v, want %s", doc.Groups[0].Proxies, want)
					}
					return
				}
				var output string
				if renderer == subscriptionRendererSingBox {
					output, err = renderSingBoxSubscription(data, custom)
				} else {
					output, err = renderZnetSinkSubscription(data, custom)
				}
				if err != nil {
					t.Fatal(err)
				}
				var doc struct {
					Outbounds []map[string]interface{} `json:"outbounds"`
					Groups    []map[string]interface{} `json:"outbound_groups"`
				}
				if err := json.Unmarshal([]byte(output), &doc); err != nil {
					t.Fatal(err)
				}
				groups := doc.Groups
				if renderer == subscriptionRendererSingBox {
					groups = doc.Outbounds
				}
				found := false
				for _, group := range groups {
					if group["tag"] != custom.PolicyGroups[0].Name {
						continue
					}
					found = true
					if group["default"] != want || group["outbounds"].([]interface{})[0] != want {
						t.Fatalf("initial member not preserved: %#v", group)
					}
				}
				if !found {
					t.Fatal("missing main selector")
				}
				if validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN")); renderer == subscriptionRendererZnetSink && validator != "" {
					path := filepath.Join(t.TempDir(), "subscription.json")
					if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
						t.Fatal(err)
					}
					if result, err := exec.Command(validator, "validate", path).CombinedOutput(); err != nil {
						t.Fatalf("Zero rejected default %s: %v\n%s", target, err, result)
					}
				}
			})
		}
	}
}

func TestSubscriptionDefaultTargetValidationAndLegacyCompatibility(t *testing.T) {
	legacy := defaultSubscriptionCustomization(subscriptionRendererClash)
	raw, _ := json.Marshal(legacy)
	current, normalized, err := normalizeSubscriptionCustomization(subscriptionRendererClash, raw)
	if err != nil {
		t.Fatal(err)
	}
	if current.PolicyGroups[0].DefaultTarget != "group:auto" || strings.Contains(string(normalized), `"default_group"`) {
		t.Fatalf("legacy default lost: %s", normalized)
	}
	for _, scenario := range []string{"direct-excluded", "reject-excluded", "missing-group", "non-select", "conflicting-legacy", "unknown-target"} {
		t.Run(scenario, func(t *testing.T) {
			custom := defaultSubscriptionCustomization(subscriptionRendererClash)
			main := &custom.PolicyGroups[0]
			main.DefaultGroup = ""
			main.DefaultTarget = "direct"
			switch scenario {
			case "direct-excluded":
				main.IncludeDirect = subscriptionBool(false)
			case "reject-excluded":
				main.DefaultTarget = "reject"
				main.IncludeReject = subscriptionBool(false)
			case "missing-group":
				main.DefaultTarget = "group:missing"
			case "non-select":
				main.Type = "urltest"
				main.ProbeURL = defaultSubscriptionProbeURL
				main.Interval = 300
			case "conflicting-legacy":
				main.DefaultGroup = "auto"
			case "unknown-target":
				main.DefaultTarget = "node:unavailable"
			}
			raw, _ := json.Marshal(custom)
			if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererClash, raw); err == nil {
				t.Fatal("accepted invalid default")
			}
		})
	}
}

func TestSubscriptionDirectOnlySelectorHasAValidDefault(t *testing.T) {
	custom := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	custom.PolicyGroups = []subscriptionPolicyGroup{{ID: "main", Name: "支付服务", Type: "select", DefaultTarget: "direct", IncludeDirect: subscriptionBool(true), IncludeReject: subscriptionBool(false)}}
	raw, _ := json.Marshal(custom)
	custom, _, err := normalizeSubscriptionCustomization(subscriptionRendererZnetSink, raw)
	if err != nil {
		t.Fatal(err)
	}
	data := subscriptionExporterTestData()
	data.ProtocolEndpoints = nil
	output, err := renderZnetSinkSubscription(data, custom)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Groups []struct {
			Default string   `json:"default"`
			Members []string `json:"outbounds"`
		} `json:"outbound_groups"`
	}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Groups) != 1 || doc.Groups[0].Default != "direct" || len(doc.Groups[0].Members) != 1 || doc.Groups[0].Members[0] != "direct" {
		t.Fatalf("unexpected direct-only selector: %s", output)
	}
}
