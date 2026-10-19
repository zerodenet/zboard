package zero

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestRenderedCredentialTargetsBelongToTheExactManagedSnapshot(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "trojan", "hysteria2", "shadowsocks", "mieru"} {
		t.Run(protocol, func(t *testing.T) {
			source := network.RuntimeConfigurationEndpoint{ID: 11, NodeID: 7, Protocol: protocol, Port: 443, MieruPrincipalReady: true, ActiveSubscriptionCount: 1, ServerConfig: `enc:{"type":"` + protocol + `","cipher":"aes-128-gcm"}`, Credentials: []network.RuntimeConfigurationCredential{{ID: 21, SubscriptionID: 31, PrincipalKey: "private-opaque-principal", Secret: "enc:private-machine-secret", PolicyRevision: 9}}}
			request := RuntimeConfigurationRenderRequest{NodeID: 7, APIKey: "test-connector", Now: time.Now(), Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: "https://panel.example", Endpoints: []network.RuntimeConfigurationEndpoint{source}}}
			payload, sha, targets, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).RenderWithCredentialTargets(request)
			if err != nil || len(targets.Credentials) != 1 || len(sha) != 64 {
				t.Fatal(len(targets.Credentials), err)
			}
			if !strings.Contains(string(payload), "private-machine-secret") {
				t.Fatal("credential was not rendered")
			}
			body, err := network.EncodeCredentialDeliveryManifest(targets.Credentials, targets.NetworkEntries)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"private-machine-secret", "private-opaque-principal", "enc:"} {
				if strings.Contains(body, private) {
					t.Fatal("private material escaped into manifest")
				}
			}
			var manifest network.CredentialDeliveryManifest
			if err := json.Unmarshal([]byte(body), &manifest); err != nil || manifest.Targets[0].CredentialID != 21 {
				t.Fatal(err)
			}
			request.Snapshot.Endpoints[0].Credentials[0].Secret = "enc:replacement-secret"
			_, _, replacement, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).RenderWithCredentialTargets(request)
			if err != nil || replacement.Credentials[0].MaterialSHA256 == targets.Credentials[0].MaterialSHA256 {
				t.Fatal("new material inherited old proof", err)
			}
		})
	}
}
