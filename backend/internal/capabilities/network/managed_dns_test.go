package network

import (
	"errors"
	"testing"
)

func TestNormalizeManagedDNSUsesNodeAddressAndCanonicalIdentity(t *testing.T) {
	record, err := normalizeManagedDNS(ManagedDNSWrite{
		ProviderAccountID: 3, NodeID: 4, DomainName: " Edge.Example.Test. ", RecordType: " a ", TTL: 0,
	}, ManagedDNSDependencies{NodeAddress: "203.0.113.10", NodeSSHHost: "2001:db8::10", ProviderKey: "cloudflare", ProviderStatus: "active", ProviderSupportsDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	if record.DomainName != "edge.example.test" || record.RecordType != "A" || record.RecordValue != "203.0.113.10" || record.TTL != 1 || record.Status != ManagedDNSPending || record.Revision != 1 || record.DesiredHash == "" {
		t.Fatalf("record=%+v", record)
	}

	v6, err := normalizeManagedDNS(ManagedDNSWrite{
		ProviderAccountID: 3, NodeID: 4, DomainName: "v6.example.test", RecordType: "AAAA", TTL: 120,
	}, ManagedDNSDependencies{NodeAddress: "203.0.113.10", NodeSSHHost: "2001:db8::10", ProviderKey: "cloudflare", ProviderStatus: "active", ProviderSupportsDNS: true})
	if err != nil || v6.RecordValue != "2001:db8::10" {
		t.Fatalf("v6=%+v error=%v", v6, err)
	}
}

func TestNormalizeManagedDNSRejectsPrivateAddressAndUnavailableProvider(t *testing.T) {
	_, err := normalizeManagedDNS(ManagedDNSWrite{
		ProviderAccountID: 3, NodeID: 4, DomainName: "edge.example.test", RecordType: "A", RecordValue: "10.0.0.1", TTL: 30,
	}, ManagedDNSDependencies{ProviderKey: "cloudflare", ProviderStatus: "invalid"})
	var validation *ManagedDNSValidation
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v", err)
	}
	for _, field := range []string{"record_value", "ttl", "provider_account_id"} {
		if validation.Fields[field] == "" {
			t.Fatalf("missing %s validation: %+v", field, validation.Fields)
		}
	}
}
