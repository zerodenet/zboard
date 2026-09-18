package handler

import "testing"

func TestParseProtocolEndpointEgressSupportsSocksAndZeroJSON(t *testing.T) {
	socks, err := parseProtocolEndpointEgress("socks5://user:pass@proxy.example:1080")
	if err != nil || socks.Protocol != "socks5" || socks.Config["server"] != "proxy.example" || socks.Config["port"] != float64(1080) || socks.Config["username"] != "user" {
		t.Fatalf("socks=%+v error=%v", socks, err)
	}
	vless, err := parseProtocolEndpointEgress(`{"protocol":{"type":"vless","server":"edge.example","port":443,"id":"11111111-2222-3333-4444-555555555555","reality":{"public_key":"key","short_id":"12","server_name":"example.com"}}}`)
	if err != nil || vless.Protocol != "vless" {
		t.Fatalf("vless=%+v error=%v", vless, err)
	}
	if _, ok := vless.Config["reality"].(map[string]interface{}); !ok {
		t.Fatalf("imported extension fields were not preserved: %+v", vless.Config)
	}
}

func TestParseProtocolEndpointEgressRejectsMultipleAndUnsupportedOutbounds(t *testing.T) {
	links := "ss://YWVzLTEyOC1nY206c2VjcmV0@one.example:8388\nss://YWVzLTEyOC1nY206c2VjcmV0@two.example:8388"
	if _, err := parseProtocolEndpointEgress(links); err == nil {
		t.Fatal("expected multiple outbound rejection")
	}
	if _, err := parseProtocolEndpointEgress(`{"type":"http","server":"proxy.example","port":8080}`); err == nil {
		t.Fatal("expected unsupported outbound rejection")
	}
}
