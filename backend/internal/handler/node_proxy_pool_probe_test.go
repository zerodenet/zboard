package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSubscriptionProbeCompatibility(t *testing.T) {
	poolValidationForTest(t)
	for _, format := range []string{"clash", "sing-box"} {
		for _, probe := range []string{"http://example.com/check", "https://example.com/check?token=must-not-downgrade"} {
			var content []byte
			if format == "clash" {
				content = []byte("proxies:\n  - {name: node, type: socks5, server: 192.0.2.1, port: 1080}\nproxy-groups:\n  - {name: auto, type: url-test, proxies: [node], interval: 300, url: '" + probe + "'}\n")
			} else {
				content = []byte(`{"outbounds":[{"tag":"node","type":"socks","server":"192.0.2.1","server_port":1080},{"tag":"auto","type":"urltest","outbounds":["node"],"url":"` + probe + `","interval":"5m"}]}`)
			}
			path, n, err := parseProxyPoolSubscription(content, "auto")
			if err != nil || n != 1 {
				t.Fatalf("%s parse failed: %v nodes=%d", format, err, n)
			}
			if probe[:5] == "https" {
				if _, ok := path.Groups[0]["url"]; ok {
					t.Fatal("HTTPS probe must use Zero default without downgrading URL")
				}
			} else if path.Groups[0]["url"] != probe {
				t.Fatal("HTTP probe must be preserved")
			}
			raw, _ := json.Marshal(path)
			if err = (&handlers{}).validateProxyPoolDocument(context.Background(), raw); err != nil {
				t.Fatalf("%s validation: %v", format, err)
			}
		}
	}
}

func TestProxyPoolValidationErrorsDoNotExposeConfig(t *testing.T) {
	old := managedZeroSubscriptionValidator
	defer func() { managedZeroSubscriptionValidator = old }()
	raw := json.RawMessage(`{"outbounds":[{"tag":"node","protocol":{"type":"socks5","server":"192.0.2.1","port":1080}}],"target":"node"}`)
	for _, tc := range []struct{ cause, want string }{
		{"resolve Zero preview validator: secret/path", "校验内核无法加载"},
		{"load Zero preview validator: secret/path", "校验内核无法加载"},
		{"Zero rejected: only supports `http://` probe urls secret", "仅支持 HTTP 测速"},
		{"Zero rejected: password=secret", "协议字段"},
	} {
		managedZeroSubscriptionValidator = func(context.Context, string, string, []byte) error { return errors.New(tc.cause) }
		err := (&handlers{}).validateProxyPoolDocument(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe or unclear error: %v", err)
		}
	}
}
