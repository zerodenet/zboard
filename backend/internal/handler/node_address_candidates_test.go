package handler

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"testing"
)

func TestIsPublicNodeAddress(t *testing.T) {
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicNodeAddress(netip.MustParseAddr(value)) {
			t.Fatalf("expected %s to be accepted", value)
		}
	}
	for _, value := range []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
		"192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "255.255.255.255",
		"::", "::1", "2001:db8::1", "fc00::1", "fe80::1", "ff02::1",
	} {
		if isPublicNodeAddress(netip.MustParseAddr(value)) {
			t.Fatalf("expected %s to be rejected", value)
		}
	}
}

func TestNodeAddressCandidateCollectorDeduplicatesAndKeepsPriority(t *testing.T) {
	collector := newNodeAddressCandidateCollector()
	if !collector.add("1.1.1.1", nodeAddressSourceNodeAddress) {
		t.Fatal("first public IPv4 candidate was not added")
	}
	collector.add("1.1.1.1", nodeAddressSourceSSHGlobal)
	collector.add("2606:4700:4700::1111", nodeAddressSourceNodeDNS)
	collector.add("10.0.0.1", nodeAddressSourceSSHGlobal)

	response := collector.response(42, nodeAddressProbeSucceeded, nil)
	if response.RecommendedIPv4 != "1.1.1.1" || response.RecommendedIPv6 != "2606:4700:4700::1111" {
		t.Fatalf("unexpected recommendations: %#v", response)
	}
	if len(response.IPv4) != 1 || response.IPv4[0].Source != nodeAddressSourceNodeAddress {
		t.Fatalf("deduplication changed source priority: %#v", response.IPv4)
	}
	if len(response.IPv6) != 1 {
		t.Fatalf("unexpected IPv6 candidates: %#v", response.IPv6)
	}
}

func TestParseNodeGlobalAddressOutputSkipsUnstableAndInvalidRows(t *testing.T) {
	output := `2: eth0    inet 1.1.1.1/24 brd 1.1.1.255 scope global eth0
2: eth0    inet 10.0.0.4/24 brd 10.0.0.255 scope global eth0
2: eth0    inet6 2606:4700:4700::1111/64 scope global dynamic
2: eth0    inet6 2606:4700:4700::1001/64 scope global temporary dynamic
2: eth0    inet6 fe80::1/64 scope link`
	addresses := parseNodeGlobalAddressOutput(output)
	want := []string{"1.1.1.1", "10.0.0.4", "2606:4700:4700::1111", "fe80::1"}
	if !reflect.DeepEqual(addresses, want) {
		t.Fatalf("parsed addresses = %#v, want %#v", addresses, want)
	}

	collector := newNodeAddressCandidateCollector()
	for _, address := range addresses {
		collector.add(address, nodeAddressSourceSSHGlobal)
	}
	if got := collector.response(1, nodeAddressProbeSucceeded, nil); len(got.IPv4) != 1 || len(got.IPv6) != 1 {
		t.Fatalf("public filtering failed: %#v", got)
	}
}

func TestResolveNodeAddressFieldUsesDeterministicPublicDNSCandidates(t *testing.T) {
	previousLookup := nodeAddressLookupIPAddrs
	nodeAddressLookupIPAddrs = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("2606:4700:4700::1111")},
			{IP: net.ParseIP("10.0.0.8")},
			{IP: net.ParseIP("8.8.8.8")},
		}, nil
	}
	defer func() { nodeAddressLookupIPAddrs = previousLookup }()

	collector := newNodeAddressCandidateCollector()
	warnings := []string{}
	resolveNodeAddressField(context.Background(), "edge.example.com", nodeAddressSourceNodeAddress, nodeAddressSourceNodeDNS, "节点地址", collector, &warnings)
	response := collector.response(1, nodeAddressProbeNotConfigured, warnings)
	if response.RecommendedIPv4 != "8.8.8.8" || response.RecommendedIPv6 != "2606:4700:4700::1111" {
		t.Fatalf("unexpected DNS recommendations: %#v", response)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}

func TestNormalizeNodeAddressHost(t *testing.T) {
	for input, want := range map[string]string{
		" [2606:4700:4700::1111] ": "2606:4700:4700::1111",
		"edge.example.com.":        "edge.example.com",
		"edge.example.com:22":      "edge.example.com",
	} {
		if got := normalizeNodeAddressHost(input); got != want {
			t.Fatalf("normalizeNodeAddressHost(%q) = %q, want %q", input, got, want)
		}
	}
}
