package zero

import (
	"strings"
	"testing"
)

func TestInspectProxyPoolConfigurationReportsDatagramCapability(t *testing.T) {
	for _, test := range []struct {
		name     string
		raw      string
		supports bool
	}{
		{"shadowsocks", `{"outbounds":[{"tag":"ss","protocol":{"type":"shadowsocks"}}],"outbound_groups":[],"target":"ss"}`, true},
		{"http", `{"outbounds":[{"tag":"http","protocol":{"type":"http","server":"192.0.2.1","port":8080}}],"outbound_groups":[],"target":"http"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts, err := InspectProxyPoolConfiguration(test.raw)
			if err != nil || facts.SupportsDatagram != test.supports {
				t.Fatalf("facts=%+v error=%v", facts, err)
			}
			if !test.supports && !strings.Contains(facts.DatagramError, "HTTP CONNECT") {
				t.Fatalf("datagram error=%q", facts.DatagramError)
			}
		})
	}
	if _, err := InspectProxyPoolConfiguration(`{"outbounds":[],"outbound_groups":[],"target":"missing","extra":true}`); err == nil {
		t.Fatal("unknown field accepted")
	}
}
