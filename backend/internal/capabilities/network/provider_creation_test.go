package network

import "testing"

func TestProviderCredentialPrefixNeverEchoesShortCredential(t *testing.T) {
	if got := providerCredentialPrefix("short"); got == "short" || got != "••••" {
		t.Fatalf("short credential prefix=%q", got)
	}
	if got := providerCredentialPrefix("long-provider-token"); got != "long…oken" {
		t.Fatalf("long credential prefix=%q", got)
	}
}
