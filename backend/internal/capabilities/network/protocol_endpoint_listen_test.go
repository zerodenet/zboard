package network

import (
	"errors"
	"testing"
)

func TestProtocolListenAddressNormalization(t *testing.T) {
	for raw, want := range map[string]string{
		"": "0.0.0.0", " 0.0.0.0 ": "0.0.0.0", "::": "::", "[::]": "::",
		"2001:0db8:0:0::10": "2001:db8::10", "192.0.2.10": "192.0.2.10", "::ffff:192.0.2.10": "192.0.2.10",
	} {
		got, err := NormalizeProtocolListenAddress(raw)
		if err != nil || got != want {
			t.Fatalf("normalize %q = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"node.example", "[::]:443", "192.0.2.10:443", "::/0", "[::", "not an IP", "fe80::1%eth0"} {
		_, err := NormalizeProtocolListenAddress(raw)
		var validation *ProtocolEndpointMutationValidation
		if !errors.As(err, &validation) || validation.Fields["listen_address"] == "" {
			t.Fatalf("invalid address %q did not produce field error: %v", raw, err)
		}
	}
}

func TestListenAddressChangePublishesRuntimeWithoutChangingDelivery(t *testing.T) {
	before := ProtocolEndpointRecord{NodeID: 1, Protocol: "vless", Address: "public.example", Port: 443, PublicPort: 443, IsActive: true}
	after := before
	after.ListenAddress = "0.0.0.0"
	if got := ClassifyProtocolEndpointChange(&before, after, 0, 0); got.Effect != ProtocolEndpointEffectNone {
		t.Fatalf("legacy empty address changed behavior: %+v", got)
	}
	after.ListenAddress = "::"
	got := ClassifyProtocolEndpointChange(&before, after, 0, 0)
	if got.Effect != ProtocolEndpointEffectRuntime || len(got.Effects) != 1 || got.PublishStatus != ProtocolEndpointPublishQueued || len(got.AffectedNodeIDs) != 1 || got.AffectedNodeIDs[0] != 1 {
		t.Fatalf("listen address did not queue only a runtime change: %+v", got)
	}
}
