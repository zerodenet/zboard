package zero

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestRuntimeRendererUsesIndependentListenAddressForEveryProtocol(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "mieru"} {
		for _, address := range []string{"", "0.0.0.0", "::", "[::]", "2001:db8::10", "192.0.2.10"} {
			t.Run(kind+"/"+address, func(t *testing.T) {
				endpoint := network.RuntimeConfigurationEndpoint{
					ID: 1, Protocol: kind, ListenAddress: address, Address: "public.example", Port: 443, PublicPort: 8443,
					MieruPrincipalReady: true, ActiveSubscriptionCount: 1,
					Credentials: []network.RuntimeConfigurationCredential{{ID: 1, PrincipalKey: "subscription:1", Secret: "enc:secret", SoleActiveCredential: true}},
				}
				protocol := map[string]interface{}{"type": kind}
				if kind == "shadowsocks" {
					protocol["cipher"] = "aes-128-gcm"
				}
				inbounds, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).renderEndpoint(endpoint, protocol, false)
				if err != nil || len(inbounds) != 1 {
					t.Fatalf("render = %v, %v", inbounds, err)
				}
				listen := inbounds[0]["listen"].(map[string]interface{})
				got := listen["address"].(string)
				want := strings.Trim(address, "[]")
				if want == "" {
					want = "0.0.0.0"
				}
				if strings.Contains(want, ":") {
					want = "[" + want + "]"
				}
				if got != want || listen["port"] != 443 {
					t.Fatalf("listen = %v, want %s:443", listen, want)
				}
				// Match the current kernel's address:port construction and ensure
				// it produces a socket address, including bracketed IPv6.
				if _, err := net.ResolveTCPAddr("tcp", got+":443"); err != nil {
					t.Fatalf("kernel cannot consume published listen address: %v", err)
				}
			})
		}
	}
}

func TestRuntimeRendererRejectsInvalidListenAndKeepsBootstrapLocal(t *testing.T) {
	renderer := RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}
	request := RuntimeConfigurationRenderRequest{NodeID: 1, APIKey: "secret", NativeConnector: true, Now: time.Now(), Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example"}}
	request.Snapshot.Endpoints = []network.RuntimeConfigurationEndpoint{{ID: 1, Protocol: "vless", Port: 443, ListenAddress: "example.com", ServerConfig: `enc:{"type":"vless"}`}}
	if _, _, err := renderer.Render(request); err == nil {
		t.Fatal("invalid stored listen address was published")
	}
	request.Snapshot.Endpoints = nil
	payload, _, err := renderer.Render(request)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct{ Listen struct{ Address string } }
	}
	if err := json.Unmarshal(payload, &config); err != nil || len(config.Inbounds) != 1 || config.Inbounds[0].Listen.Address != "127.0.0.1" {
		t.Fatalf("bootstrap listener changed: %s, %v", payload, err)
	}
}
