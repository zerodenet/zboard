package handler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestVersionTwoCustomizationMigratesToRuntimeDefaults(t *testing.T) {
	raw := json.RawMessage(`{"version":2,"mixed_port":7891,"main_group":"main","policy_groups":[{"id":"main","name":"Main","type":"select"}],"final":"group:main","rule_sets":[]}`)
	customization, normalized, err := normalizeSubscriptionCustomization(subscriptionRendererSingBox, raw)
	if err != nil {
		t.Fatal(err)
	}
	if customization.Version != 3 || customization.Mode != subscriptionModeRule || customization.MixedPort != 7891 {
		t.Fatalf("migrated customization = %#v", customization)
	}
	if customization.DNS.Enabled || customization.Tun.Enabled || customization.SystemProxy {
		t.Fatalf("migration unexpectedly enabled runtime capture: %#v", customization)
	}
	if !json.Valid(normalized) || !slices.Contains(customization.Tun.Addresses, "10.66.0.1/24") {
		t.Fatalf("normalized migration = %s", normalized)
	}
}

func TestZeroRendererEmitsModeDNSAndTun(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.Mode = subscriptionModeGlobal
	customization.DNS.Enabled = true
	customization.DNS.Servers = []subscriptionDNSServer{{Tag: "cloudflare", Type: "udp", Host: "1.1.1.1", Port: 53}}
	customization.DNS.DefaultServer = "cloudflare"
	customization.DNS.FakeIPEnabled = true
	customization.Tun.Enabled = true
	rendered, err := renderZnetSinkSubscription(subscriptionExporterTestData(), customization)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]interface{}
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatal(err)
	}
	mode := document["mode"].(map[string]interface{})
	if mode["type"] != "global" || mode["outbound"] != "节点选择" {
		t.Fatalf("Zero mode = %#v", mode)
	}
	runtime := document["runtime"].(map[string]interface{})
	dns := runtime["dns"].(map[string]interface{})
	tun := runtime["tun"].(map[string]interface{})
	if dns["default_server"] != "cloudflare" || tun["dns_hijack"] != true || tun["secondary_addr"] != "fd66::1/64" {
		t.Fatalf("Zero runtime = %#v", runtime)
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN")); validator != "" {
		configPath := filepath.Join(t.TempDir(), "zboard-zero-runtime.json")
		if err := os.WriteFile(configPath, []byte(rendered), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(validator, "validate", configPath).CombinedOutput(); err != nil {
			t.Fatalf("zero validate failed: %v\n%s\n%s", err, output, rendered)
		}
	}
}

func TestSingBoxRendererEmitsCurrentRuntimeShape(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererSingBox)
	customization.Mode = subscriptionModeDirect
	customization.SystemProxy = true
	customization.DNS.Enabled = true
	customization.DNS.Servers = []subscriptionDNSServer{{
		Tag: "cloudflare", Type: "doh", Host: "1.1.1.1", Port: 443, Path: "/dns-query", ServerName: "cloudflare-dns.com",
	}}
	customization.DNS.DefaultServer = "cloudflare"
	customization.Tun.Enabled = true
	rendered, err := renderSingBoxSubscription(subscriptionExporterTestData(), customization)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		DNS       map[string]interface{}   `json:"dns"`
		Inbounds  []map[string]interface{} `json:"inbounds"`
		Outbounds []map[string]interface{} `json:"outbounds"`
		Route     map[string]interface{}   `json:"route"`
	}
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Inbounds) != 2 || document.Inbounds[0]["set_system_proxy"] != true || document.Inbounds[1]["type"] != "tun" {
		t.Fatalf("sing-box inbounds = %#v", document.Inbounds)
	}
	if _, exists := document.Inbounds[1]["dns_mode"]; exists {
		t.Fatalf("stable sing-box config must not require the 1.14-only dns_mode field: %#v", document.Inbounds[1])
	}
	if document.Route["final"] != "direct" || document.Route["auto_detect_interface"] != true {
		t.Fatalf("sing-box route = %#v", document.Route)
	}
	rules := document.Route["rules"].([]interface{})
	if len(rules) < 2 || rules[1].(map[string]interface{})["action"] != "hijack-dns" {
		t.Fatalf("sing-box DNS hijack rule = %#v", rules)
	}
	for _, outbound := range document.Outbounds {
		if outbound["type"] == "block" {
			t.Fatalf("sing-box current config retained deprecated block outbound: %#v", outbound)
		}
	}
	servers := document.DNS["servers"].([]interface{})
	server := servers[0].(map[string]interface{})
	if server["type"] != "https" || server["path"] != "/dns-query" {
		t.Fatalf("sing-box DNS server = %#v", server)
	}
	if validator := strings.TrimSpace(os.Getenv("ZBOARD_SING_BOX_VALIDATE_BIN")); validator != "" {
		configPath := filepath.Join(t.TempDir(), "zboard-sing-box-runtime.json")
		if err := os.WriteFile(configPath, []byte(rendered), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(validator, "check", "-c", configPath).CombinedOutput(); err != nil {
			t.Fatalf("sing-box check failed: %v\n%s\n%s", err, output, rendered)
		}
	}
}

func TestClashRendererEmitsDNSAndTun(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererClash)
	customization.DNS.Enabled = true
	customization.DNS.Servers = []subscriptionDNSServer{{
		Tag: "cloudflare", Type: "doh", Host: "1.1.1.1", Port: 443, Path: "/dns-query", ServerName: "cloudflare-dns.com",
	}}
	customization.DNS.DefaultServer = "cloudflare"
	customization.DNS.FakeIPEnabled = true
	customization.Tun.Enabled = true
	rendered, err := renderClashSubscription(subscriptionExporterTestData(), customization)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatal(err)
	}
	dns := document["dns"].(map[interface{}]interface{})
	if dns["enhanced-mode"] != "fake-ip" || dns["fake-ip-range"] != "198.18.0.0/15" {
		t.Fatalf("Clash DNS = %#v", dns)
	}
	nameservers := dns["nameserver"].([]interface{})
	if len(nameservers) != 1 || nameservers[0] != "https://1.1.1.1:443/dns-query#name-cert-verify=cloudflare-dns.com" {
		t.Fatalf("Clash nameservers = %#v", nameservers)
	}
	tun := document["tun"].(map[interface{}]interface{})
	if tun["enable"] != true || tun["stack"] != "mixed" || tun["inet6-address"] != "fd66::1/64" {
		t.Fatalf("Clash TUN = %#v", tun)
	}
	dnsHijack := tun["dns-hijack"].([]interface{})
	if len(dnsHijack) != 2 || dnsHijack[1] != "tcp://any:53" {
		t.Fatalf("Clash TUN DNS hijack = %#v", dnsHijack)
	}
}

func TestFullConfigRenderersCanDisableMixedInboundWhenTunIsEnabled(t *testing.T) {
	data := subscriptionExporterTestData()
	for _, renderer := range []string{subscriptionRendererZnetSink, subscriptionRendererClash, subscriptionRendererSingBox} {
		customization := defaultSubscriptionCustomization(renderer)
		customization.MixedEnabled = false
		customization.Tun.Enabled = true
		customization.DNS.Enabled = true
		raw, err := json.Marshal(customization)
		if err != nil {
			t.Fatal(err)
		}
		normalized, _, err := normalizeSubscriptionCustomization(renderer, raw)
		if err != nil {
			t.Fatalf("normalize %s: %v", renderer, err)
		}
		rendered, _, err := renderSubscriptionWithRenderer(renderer, mustMarshalJSON(t, normalized), data)
		if err != nil {
			t.Fatalf("render %s: %v", renderer, err)
		}
		switch renderer {
		case subscriptionRendererZnetSink:
			var document struct {
				Inbounds []interface{} `json:"inbounds"`
			}
			if err := json.Unmarshal([]byte(rendered), &document); err != nil || len(document.Inbounds) != 0 {
				t.Fatalf("Zero inbounds = %#v, err = %v", document.Inbounds, err)
			}
		case subscriptionRendererClash:
			if strings.Contains(rendered, "mixed-port:") || strings.Contains(rendered, "bind-address:") {
				t.Fatalf("Clash retained mixed inbound:\n%s", rendered)
			}
		case subscriptionRendererSingBox:
			var document struct {
				Inbounds []map[string]interface{} `json:"inbounds"`
			}
			if err := json.Unmarshal([]byte(rendered), &document); err != nil || len(document.Inbounds) != 1 || document.Inbounds[0]["type"] != "tun" {
				t.Fatalf("sing-box inbounds = %#v, err = %v", document.Inbounds, err)
			}
		}
	}
}

func mustMarshalJSON(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFullConfigRejectsNoTrafficCaptureEntry(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererClash)
	customization.MixedEnabled = false
	raw, err := json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererClash, raw); err == nil || !strings.Contains(err.Error(), "不能同时关闭") {
		t.Fatalf("normalize error = %v", err)
	}
}

func TestTunDNSHijackRequiresDNS(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.Tun.Enabled = true
	raw, err := json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererZnetSink, raw); err == nil {
		t.Fatal("TUN DNS hijack without DNS unexpectedly accepted")
	}
}

func TestZeroRendererRejectsUnsupportedTCPDNS(t *testing.T) {
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.DNS.Enabled = true
	customization.DNS.Servers = []subscriptionDNSServer{{Tag: "tcp", Type: "tcp", Host: "1.1.1.1", Port: 53}}
	customization.DNS.DefaultServer = "tcp"
	raw, err := json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := normalizeSubscriptionCustomization(subscriptionRendererZnetSink, raw); err == nil || !strings.Contains(err.Error(), "不支持 TCP DNS") {
		t.Fatalf("normalizeSubscriptionCustomization() error = %v", err)
	}
}
