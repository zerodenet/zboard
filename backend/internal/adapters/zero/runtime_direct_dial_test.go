package zero

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestRendererScopesDirectDialToNamedEndpointOutbound(t *testing.T) {
	data, _, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).Render(RuntimeConfigurationRenderRequest{NodeID: 1, APIKey: "test", Now: time.Now(), Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example", Endpoints: []network.RuntimeConfigurationEndpoint{{ID: 7, Protocol: "vless", Port: 8443, ServerConfig: `enc:{"type":"vless","users":[]}`, EgressConfig: `enc:{"type":"direct","dial":{"address_family":"only_ipv6","interface":"eth1","source_ip":"2001:db8::1"}}`, Credentials: []network.RuntimeConfigurationCredential{{ID: 1, SubscriptionID: 1, PolicyRevision: 1, PrincipalKey: "subscription:1:endpoint:7", Secret: "enc:11111111-2222-3333-4444-555555555555"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Outbounds []struct {
			Tag      string         `json:"tag"`
			Protocol map[string]any `json:"protocol"`
			Dial     map[string]any `json:"dial"`
		} `json:"outbounds"`
		Route struct {
			Rules []struct {
				Action struct {
					Outbound string `json:"outbound"`
				} `json:"action"`
			} `json:"rules"`
			Final map[string]any `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Outbounds) != 1 || config.Outbounds[0].Protocol["type"] != "direct" || config.Outbounds[0].Dial["address_family"] != "only_ipv6" || config.Outbounds[0].Protocol["dial"] != nil {
		t.Fatal("dial projected into the wrong native layer")
	}
	if config.Route.Rules[0].Action.Outbound != config.Outbounds[0].Tag || config.Route.Final["type"] != "direct" {
		t.Fatal("endpoint policy affected the global direct default")
	}
}

func TestRendererUsesEntryLocalIPv6ListenWithoutChangingPublicAddress(t *testing.T) {
	data, _, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).Render(RuntimeConfigurationRenderRequest{NodeID: 1, APIKey: "test", Now: time.Now(), Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example", NetworkEntries: []network.RuntimeConfigurationNetworkEntry{{ID: 9, Port: 10000, ListenAddress: "::", Network: "tcp_udp", Landing: network.RuntimeConfigurationLanding{EndpointExists: true, NodeExists: true, Active: true, NodeEnabled: true, Protocol: "vless", Address: "2001:db8::2", PublicPort: 443}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Listen struct {
				Address string `json:"address"`
			} `json:"listen"`
			Protocol struct {
				Target string `json:"target"`
			} `json:"protocol"`
		} `json:"inbounds"`
	}
	json.Unmarshal(data, &config)
	if config.Inbounds[0].Listen.Address != "::" || config.Inbounds[0].Protocol.Target != "2001:db8::2" {
		t.Fatal("entry local listen and remote target were conflated")
	}
}
