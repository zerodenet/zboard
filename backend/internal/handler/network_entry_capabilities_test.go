package handler

import (
	"encoding/json"
	"testing"
)

func TestDirectUDPRequiresDeclaredKernelCapability(t *testing.T) {
	for _, tc := range []struct {
		info string
		want bool
	}{
		{`build_id: 0.0.15`, false},
		{`protocol_capabilities: invalid`, false},
		{`protocol_capabilities: [{"protocol":"direct","compiled":true,"inbound":{"udp":{"supported":true}}}]`, true},
		{`protocol_capabilities: [{"protocol":"direct","compiled":true,"inbound":{"udp":{"supported":false}}}]`, false},
		{`protocol_capabilities: [{"protocol":"direct","compiled":false,"inbound":{"udp":{"supported":true}}}]`, false},
		{`protocol_capabilities: [{"protocol":"shadowsocks","compiled":true,"inbound":{"udp":{"supported":true}}}]`, false},
	} {
		if got := zeroBuildSupportsDirectUDP(tc.info); got != tc.want {
			t.Fatalf("capability %q = %v, want %v", tc.info, got, tc.want)
		}
	}
}

func TestDirectUDPRequirementUsesExistingPolicy(t *testing.T) {
	for _, tc := range []struct {
		config string
		want   bool
	}{
		{`{"inbounds":[{"protocol":{"type":"direct"}}]}`, true},
		{`{"inbounds":[{"protocol":{"type":"direct"},"udp":{"enabled":false}}]}`, false},
		{`{"runtime":{"udp":{"enabled":false}},"inbounds":[{"protocol":{"type":"direct"},"udp":{"enabled":true}}]}`, false},
		{`{"inbounds":[{"protocol":{"type":"socks5"}}],"outbounds":[{"protocol":{"type":"direct"}}]}`, false},
	} {
		if got := requiresDirectInboundUDP([]byte(tc.config)); got != tc.want {
			t.Fatalf("requirement %s = %v, want %v", tc.config, got, tc.want)
		}
	}
}

func TestNetworkEntryRuntimeUsesUDPPolicyForBothModes(t *testing.T) {
	for _, mode := range []string{"tcp", "tcp_udp"} {
		t.Run(mode, func(t *testing.T) {
			f, a, b := networkEntryFixture(t)
			saveEntryForTest(t, f, a, b, `,"network":"`+mode+`"`)
			payload, _, err := f.h.compileNodeRuntimeConfig(a, "fixture", "0.1.0")
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Inbounds []struct {
					UDP struct {
						Enabled bool `json:"enabled"`
					} `json:"udp"`
					Protocol map[string]interface{} `json:"protocol"`
				} `json:"inbounds"`
			}
			if err := json.Unmarshal(payload, &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Inbounds) != 1 {
				t.Fatalf("inbounds: %s", payload)
			}
			inbound := config.Inbounds[0]
			if inbound.UDP.Enabled != (mode != "tcp") {
				t.Fatalf("UDP policy: %s", payload)
			}
			if _, found := inbound.Protocol["network"]; found {
				t.Fatalf("duplicate network selector: %s", payload)
			}
		})
	}
}
