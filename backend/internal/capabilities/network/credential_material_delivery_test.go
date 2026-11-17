package network

import (
	"strings"
	"testing"
)

func TestMaterialDeliverySharesOneRootAcrossEndpointsWithoutChangingLegacyEvidence(t *testing.T) {
	a := CredentialDeliveryTarget{CredentialID: 7, ProtocolEndpointID: 9, MaterialSHA256: strings.Repeat("a", 64), EndpointSHA256: strings.Repeat("b", 64), PolicyRevision: 1}
	old, e := EncodeCredentialDeliveryManifest([]CredentialDeliveryTarget{a})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(old, `"version":1`) || strings.Contains(old, "material_version") {
		t.Fatal("changed frozen legacy format")
	}
	if _, e := DecodeConfigurationDeliveryManifest(old); e != nil {
		t.Fatal(e)
	}
	b := a
	b.ProtocolEndpointID = 10
	if _, e := EncodeCredentialDeliveryManifest([]CredentialDeliveryTarget{a, b}); e == nil {
		t.Fatal("legacy ID admitted duplicate bindings")
	}
	a.MaterialVersion, b.MaterialVersion = 1, 1
	body, e := EncodeCredentialDeliveryManifest([]CredentialDeliveryTarget{b, a})
	if e != nil {
		t.Fatal(e)
	}
	got, e := DecodeConfigurationDeliveryManifest(body)
	if e != nil || got.Version != 2 || len(got.Targets) != 2 || got.Targets[0].ProtocolEndpointID != 9 {
		t.Fatal(got, e)
	}
	if _, e := EncodeCredentialDeliveryManifest([]CredentialDeliveryTarget{a, a}); e == nil {
		t.Fatal("duplicate root binding")
	}
	b.MaterialVersion = 0
	if _, e := EncodeCredentialDeliveryManifest([]CredentialDeliveryTarget{a, b}); e == nil {
		t.Fatal("mixed identity kinds")
	}
}
