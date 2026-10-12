package network

import (
	"encoding/json"
	"testing"
)

func TestDirectEgressNormalizesHardConstraints(t *testing.T) {
	kind, raw, err := NormalizeProtocolEndpointEgressConfig(`{"type":"DIRECT","dial":{"address_family":"only_ipv6","interface":"eth1","source_ip":"2001:db8::1"}}`)
	if err != nil || kind != "direct" {
		t.Fatalf("%s %s %v", kind, raw, err)
	}
	var document map[string]any
	json.Unmarshal([]byte(raw), &document)
	if document["type"] != "direct" || document["dial"].(map[string]any)["source_ip"] != "2001:db8::1" {
		t.Fatal("dial constraints lost")
	}
	if kind, raw, err := NormalizeProtocolEndpointEgressConfig(`{"type":"direct","dial":{"address_family":"auto"}}`); err != nil || kind != "" || raw != "" {
		t.Fatalf("default direct is not a no-op: %v", err)
	}
	_, raw, err = NormalizeProtocolEndpointEgressConfig(`{"type":"direct","dial":{"source_ip":"::ffff:192.0.2.1"}}`)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(raw), &document)
	if document["dial"].(map[string]any)["source_ip"] != "192.0.2.1" {
		t.Fatal("mapped source not canonicalized")
	}
}

func TestDirectEgressRejectsUnenforceableOrConflictingDial(t *testing.T) {
	for _, raw := range []string{
		`{"type":"direct","dial":{"address_family":"only_ipv6","source_ip":"192.0.2.1"}}`,
		`{"type":"direct","dial":{"source_ip":"::"}}`,
		`{"type":"direct","dial":{"source_ip":"ff02::1"}}`,
		`{"type":"direct","dial":{"source_ip":"fe80::1%eth0"}}`,
		`{"type":"direct","dial":{"source_ip":"192.0.2.1/24"}}`,
		`{"type":"direct","dial":{"source_ip":"255.255.255.255"}}`,
		`{"type":"direct","dial":{"interface":" eth0"}}`,
		`{"type":"direct","dial":{"interface":""}}`,
		`{"type":"direct","dial":{"address_family":null}}`,
		`{"type":"direct","dial":null}`,
		`{"type":"direct","bind_interface":"eth0"}`,
		`{"type":"socks5","server":"proxy.example","port":1080,"dial":{"address_family":"only_ipv6"}}`,
	} {
		if _, _, err := NormalizeProtocolEndpointEgressConfig(raw); err == nil {
			t.Fatalf("unsafe egress admitted: %s", raw)
		}
	}
}
