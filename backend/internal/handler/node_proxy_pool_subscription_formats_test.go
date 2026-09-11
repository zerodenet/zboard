package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestProxyPoolAdditionalFormats(t *testing.T) {
	poolValidationForTest(t)
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"ps":"vm","add":"198.51.100.1","port":"443","id":"bf000d23-0752-40b4-affe-68f7707a9661","aid":"0","scy":"auto","net":"ws","type":"none","sni":"example.com","path":"/proxy","tls":"tls"}`))
	cases := []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:password")) + "@198.51.100.1:443#same",
		"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:password@198.51.100.1:443")),
		"vless://bf000d23-0752-40b4-affe-68f7707a9661@[2001:db8::1]:443?security=tls&type=grpc&serviceName=proxy&sni=example.com",
		"vmess://" + vmess,
		"trojan://p%40ssword@198.51.100.1:443?sni=example.com",
		"hy2://password@198.51.100.1?sni=example.com&insecure=1",
		`{"outbounds":[{"type":"shadowsocks","tag":"ss","server":"198.51.100.1","server_port":443,"method":"aes-128-gcm","password":"password"},{"type":"selector","tag":"select","outbounds":["ss"],"default":"ss"}],"route":{"final":"ignored"}}`,
	}
	for _, raw := range cases {
		for _, content := range []string{raw, base64.StdEncoding.EncodeToString([]byte(raw))} {
			path, count, err := parseProxyPoolSubscription([]byte(content), "auto")
			if err != nil || count != 1 {
				t.Fatalf("parse %s count=%d err=%v", raw, count, err)
			}
			compiled := map[string]interface{}{"inbounds": []interface{}{}}
			target, err := path.appendGraph(compiled, "test/")
			if err != nil {
				t.Fatal(err)
			}
			compiled["route"] = map[string]interface{}{"final": map[string]interface{}{"type": "route", "outbound": target}}
			payload, _ := json.Marshal(compiled)
			if err := managedZeroSubscriptionValidator(context.Background(), "", "", payload); err != nil {
				t.Fatalf("Zero rejected %s: %v", payload, err)
			}
		}
	}
	path, count, err := parseProxyPoolSubscription([]byte(cases[0]+"\n"+cases[0]), "links")
	if err != nil || count != 2 || path.Outbounds[0]["tag"] == path.Outbounds[1]["tag"] {
		t.Fatalf("duplicate labels lost: %v", err)
	}
}
func TestProxyPoolRejectsUnsupportedConversion(t *testing.T) {
	for _, raw := range []string{
		`{"outbounds":[{"type":"shadowsocks","server":"x","server_port":443,"method":"aes-128-gcm","password":"p"},{"type":"tuic","server":"x","server_port":443}]}`,
		`{"outbounds":[{"type":"shadowsocks","server":"x","server_port":443,"plugin":"obfs-local"}]}`,
		`{"outbounds":[{"type":"vless","server":"x","server_port":443,"transport":{"type":"quic"}}]}`,
		"ss://aes-128-gcm:password@host:443?plugin=obfs-local", "hy2://password@host:443?obfs=salamander", "vless://id@host:443?type=kcp",
	} {
		path, count, err := parseProxyPoolSubscription([]byte(raw), "auto")
		if err == nil || count != 0 || len(path.Outbounds) != 0 {
			t.Fatalf("silently imported %s", raw)
		}
	}
}
