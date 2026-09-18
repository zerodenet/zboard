package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolCredentialClientPortUsesEndpointForShadowsocks(t *testing.T) {
	endpoint := model.ProtocolEndpoint{Protocol: "shadowsocks", PublicPort: 12855}
	credential := model.ProtocolCredential{PublicPort: 12857}
	if got := protocolCredentialClientPort(endpoint, credential); got != 12855 {
		t.Fatalf("port = %d, want 12855", got)
	}
	endpoint.Protocol = "trojan"
	if got := protocolCredentialClientPort(endpoint, credential); got != 12857 {
		t.Fatalf("non-Shadowsocks port = %d, want 12857", got)
	}
}
