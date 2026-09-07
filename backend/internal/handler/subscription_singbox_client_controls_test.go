package handler

import (
	"encoding/json"
	"testing"
)

func TestSingBoxClientModesKeepRulesAndRuleFallbackForEveryInitialMode(t *testing.T) {
	for _, mode := range []string{subscriptionModeRule, subscriptionModeDirect, subscriptionModeGlobal} {
		for _, withRules := range []bool{false, true} {
			t.Run(mode+map[bool]string{true: "/rules", false: "/empty"}[withRules], func(t *testing.T) {
				customization := defaultSubscriptionCustomization(subscriptionRendererSingBox)
				customization.Mode = mode
				customization.Final = subscriptionTargetDirect
				if withRules {
					customization.RuleSets = []subscriptionRuleSetCustomization{
						{Tag: "ads", URL: "https://example.com/ads.srs", Format: "binary", Target: subscriptionTargetReject, Interval: 3600},
						{Tag: "proxy", URL: "https://example.com/proxy.srs", Format: "binary", Target: "group:main", Interval: 3600},
					}
				}
				rendered, err := renderSingBoxSubscription(subscriptionExporterTestData(), customization)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Experimental struct {
						ClashAPI map[string]interface{} `json:"clash_api"`
					} `json:"experimental"`
					Route struct {
						Rules []map[string]interface{} `json:"rules"`
						Final string                   `json:"final"`
					} `json:"route"`
				}
				if err := json.Unmarshal([]byte(rendered), &document); err != nil {
					t.Fatal(err)
				}
				if document.Experimental.ClashAPI["default_mode"] != mode {
					t.Fatalf("initial mode = %#v", document.Experimental)
				}
				if _, exists := document.Experimental.ClashAPI["external_controller"]; exists {
					t.Fatal("client controls must not expose a controller listener")
				}
				rules := document.Route.Rules
				if len(rules) != len(customization.RuleSets)+3 {
					t.Fatalf("lost rules: %#v", rules)
				}
				for index, want := range []struct{ mode, target string }{{"direct", "direct"}, {"global", "节点选择"}} {
					if rules[index]["clash_mode"] != want.mode || rules[index]["outbound"] != want.target || rules[index]["action"] != "route" {
						t.Fatalf("mode route = %#v", rules[index])
					}
				}
				if withRules && (rules[2]["action"] != "reject" || rules[3]["outbound"] != "节点选择") {
					t.Fatalf("rule order = %#v", rules)
				}
				last := rules[len(rules)-1]
				if last["clash_mode"] != "rule" || last["outbound"] != "direct" || document.Route.Final != "direct" {
					t.Fatalf("rule fallback = %#v", document.Route)
				}
			})
		}
	}
}

func TestSingBoxClientHTTPProxyRequiresTunAndMixedAndUsesConfiguredPort(t *testing.T) {
	for _, tun := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			for _, system := range []bool{false, true} {
				customization := defaultSubscriptionCustomization(subscriptionRendererSingBox)
				customization.Tun.Enabled, customization.MixedEnabled, customization.SystemProxy = tun, mixed, system
				customization.MixedPort = 17890
				foundPlatform := false
				for _, inbound := range singBoxSubscriptionInbounds(customization) {
					if inbound["type"] == "mixed" && (inbound["set_system_proxy"] == true) != system {
						t.Fatalf("CLI proxy setting lost: %#v", inbound)
					}
					if platform, ok := inbound["platform"].(map[string]interface{}); ok {
						foundPlatform = true
						proxy := platform["http_proxy"].(map[string]interface{})
						if proxy["enabled"] != true || proxy["server"] != "127.0.0.1" || proxy["server_port"] != 17890 {
							t.Fatalf("HTTP proxy = %#v", proxy)
						}
					}
				}
				if foundPlatform != (tun && mixed) {
					t.Fatalf("tun=%v mixed=%v system=%v platform=%v", tun, mixed, system, foundPlatform)
				}
			}
		}
	}
}
