package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func renderNativeBuiltinTest(t *testing.T, renderer string, customization subscriptionTemplateCustomization) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := renderSubscriptionWithRenderer(renderer, raw, subscriptionExporterTestData())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]interface{}
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestNativeSubscriptionBuiltinsHaveOneCanonicalTag(t *testing.T) {
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererSingBox} {
		t.Run(renderer, func(t *testing.T) {
			document := renderNativeBuiltinTest(t, renderer, defaultSubscriptionCustomization(renderer))
			counts := map[string]int{}
			for _, item := range document["outbounds"].([]interface{}) {
				outbound := item.(map[string]interface{})
				counts[outbound["tag"].(string)]++
			}
			if counts["direct"] != 1 || counts["DIRECT"] != 0 || counts["REJECT"] != 0 {
				t.Fatalf("builtin counts = %v", counts)
			}
			if renderer == subscriptionRendererZnetSink && counts["block"] != 1 {
				t.Fatalf("Zero block counts = %v", counts)
			}
			if renderer == subscriptionRendererSingBox && counts["block"] != 0 {
				t.Fatalf("sing-box unexpectedly added a block outbound: %v", counts)
			}
		})
	}
}

func TestNativeSubscriptionPreservesLegacyAppleDefaultAndRejectAction(t *testing.T) {
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererSingBox} {
		t.Run(renderer, func(t *testing.T) {
			customization := defaultSubscriptionCustomization(renderer)
			baseline := renderNativeBuiltinTest(t, renderer, customization)
			apple := map[string]interface{}{"tag": "Apple 服务", "type": "selector", "default": "DIRECT", "outbounds": []string{"DIRECT", "direct", "节点选择", "$zboard:all-nodes"}}
			var overlay map[string]interface{}
			if renderer == subscriptionRendererZnetSink {
				groups := baseline["outbound_groups"].([]interface{})
				overlay = map[string]interface{}{
					"outbound_groups": append(groups, apple),
					"outbounds":       []interface{}{map[string]interface{}{"$zboard": "generated-outbounds"}, map[string]interface{}{"tag": "DIRECT", "protocol": map[string]interface{}{"type": "direct"}}, map[string]interface{}{"tag": "REJECT", "protocol": map[string]interface{}{"type": "block"}}},
					"route": map[string]interface{}{"final": map[string]interface{}{"type": "route", "outbound": "DIRECT"}, "rules": []interface{}{
						map[string]interface{}{"condition": map[string]interface{}{"type": "domain", "values": []string{"REJECT"}}, "action": map[string]interface{}{"type": "reject"}},
						map[string]interface{}{"condition": map[string]interface{}{"type": "domain", "values": []string{"example.test"}}, "action": map[string]interface{}{"type": "route", "outbound": "REJECT"}},
					}},
					"mode":    map[string]interface{}{"type": "global", "outbound": "DIRECT"},
					"runtime": map[string]interface{}{"dns": map[string]interface{}{"servers": []interface{}{map[string]interface{}{"tag": "DIRECT", "detour": "DIRECT"}}}},
				}
			} else {
				overlay = map[string]interface{}{
					"outbounds": []interface{}{map[string]interface{}{"tag": "DIRECT", "type": "direct"}, map[string]interface{}{"$zboard": "generated-outbounds"}, apple},
					"route":     map[string]interface{}{"final": "DIRECT", "rules": []interface{}{map[string]interface{}{"action": "reject"}, map[string]interface{}{"action": "route", "outbound": "DIRECT"}}},
					"dns":       map[string]interface{}{"servers": []interface{}{map[string]interface{}{"tag": "DIRECT", "type": "udp", "server": "dns.example.test", "detour": "DIRECT"}}},
				}
			}
			raw, _ := json.Marshal(overlay)
			customization.AdvancedSource = string(raw)
			document := renderNativeBuiltinTest(t, renderer, customization)
			groupField := "outbounds"
			if renderer == subscriptionRendererZnetSink {
				groupField = "outbound_groups"
			}
			found := false
			for _, item := range document[groupField].([]interface{}) {
				group := item.(map[string]interface{})
				if group["tag"] != "Apple 服务" {
					continue
				}
				found = true
				members := group["outbounds"].([]interface{})
				if group["default"] != "direct" || members[0] != "direct" || members[1] != "节点选择" {
					t.Fatalf("Apple default or ordering changed: %#v", group)
				}
				for _, member := range members[1:] {
					if member == "direct" || member == "DIRECT" {
						t.Fatalf("duplicate Apple direct member: %v", members)
					}
				}
			}
			if !found {
				t.Fatal("Apple selector missing")
			}
			encoded, _ := json.Marshal(document)
			if strings.Contains(string(encoded), `"detour":"DIRECT"`) || strings.Contains(string(encoded), `"outbound":"DIRECT"`) {
				t.Fatalf("legacy reference remains: %s", encoded)
			}
			route := document["route"].(map[string]interface{})
			first := route["rules"].([]interface{})[0].(map[string]interface{})
			if renderer == subscriptionRendererZnetSink {
				if first["action"].(map[string]interface{})["type"] != "reject" || route["final"].(map[string]interface{})["outbound"] != "direct" {
					t.Fatalf("Zero actions changed: %#v", route)
				}
				if first["condition"].(map[string]interface{})["values"].([]interface{})[0] != "REJECT" {
					t.Fatal("domain value was rewritten as a target")
				}
			} else if first["action"] != "reject" || route["final"] != "direct" {
				t.Fatalf("sing-box actions changed: %#v", route)
			}
		})
	}
}

func TestNativeSubscriptionKeepsCaseSensitiveCustomTargets(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.AdvancedSource = `{"outbounds":[{"$zboard":"generated-outbounds"},{"tag":"DIRECT","protocol":{"type":"socks5","server":"proxy.test","port":1080}},{"tag":"Direct","protocol":{"type":"direct"}}],"outbound_groups":[{"tag":"REJECT","type":"selector","default":"DIRECT","outbounds":["DIRECT","Direct","direct"]}],"route":{"final":{"type":"route","outbound":"REJECT"}}}`
	document := renderNativeBuiltinTest(t, subscriptionRendererZnetSink, customization)
	group := document["outbound_groups"].([]interface{})[0].(map[string]interface{})
	if group["tag"] != "REJECT" || group["default"] != "DIRECT" || !reflect.DeepEqual(group["outbounds"], []interface{}{"DIRECT", "Direct", "direct"}) {
		t.Fatalf("custom targets were case folded: %#v", group)
	}
	if document["route"].(map[string]interface{})["final"].(map[string]interface{})["outbound"] != "REJECT" {
		t.Fatal("custom group reference was rewritten")
	}
}

func TestNativeSubscriptionRejectsConflictingBuiltinAliasConfiguration(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.AdvancedSource = `{"outbounds":[{"$zboard":"generated-outbounds"},{"tag":"DIRECT","protocol":{"type":"direct"},"dial":{"interface":"custom-interface"}}]}`
	_, err := renderZnetSinkSubscription(subscriptionExporterTestData(), customization)
	if err == nil || !strings.Contains(err.Error(), "配置冲突") {
		t.Fatalf("conflicting alias silently discarded: %v", err)
	}
}

func TestNativeSubscriptionSelectionPreferencesUseCanonicalTargets(t *testing.T) {
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererSingBox} {
		t.Run(renderer, func(t *testing.T) {
			customization := defaultSubscriptionCustomization(renderer)
			customization.PolicyGroups[0].IncludeDirect = subscriptionBool(false)
			customization.PolicyGroups[0].IncludeReject = subscriptionBool(false)
			customization.AdvancedSource = `{"outbounds":[{"$zboard":"generated-outbounds"}]}`
			if renderer == subscriptionRendererZnetSink {
				customization.AdvancedSource = `{"outbound_groups":[{"tag":"节点选择","type":"selector","outbounds":["自动选择","DIRECT","REJECT"]},{"tag":"自动选择","type":"selector","outbounds":["DIRECT"]}]}`
			} else {
				customization.AdvancedSource = `{"outbounds":[{"$zboard":"generated-outbounds"},{"tag":"custom","type":"selector","outbounds":["DIRECT"]}]}`
			}
			document := renderNativeBuiltinTest(t, renderer, customization)
			field := "outbounds"
			if renderer == subscriptionRendererZnetSink {
				field = "outbound_groups"
			}
			for _, item := range document[field].([]interface{}) {
				group := item.(map[string]interface{})
				if group["tag"] == "节点选择" {
					for _, member := range group["outbounds"].([]interface{}) {
						if member == "direct" || member == "block" {
							t.Fatalf("excluded builtin remains: %#v", group)
						}
					}
				}
			}
		})
	}
}
