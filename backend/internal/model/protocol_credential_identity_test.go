package model

import (
	"strings"
	"testing"
)

func TestProtocolCredentialBeforeCreateReplacesDatabaseDerivedPrincipal(t *testing.T) {
	credential := ProtocolCredential{
		SubscriptionID:     1,
		ProtocolEndpointID: 3,
		PrincipalKey:       "subscription:1:endpoint:3",
	}
	if err := credential.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate: %v", err)
	}
	if !strings.HasPrefix(credential.PrincipalKey, "principal:") {
		t.Fatalf("principal key %q is not opaque", credential.PrincipalKey)
	}
	if strings.Contains(credential.PrincipalKey, "subscription:1") || strings.Contains(credential.PrincipalKey, "endpoint:3") {
		t.Fatalf("principal key leaks database identifiers: %q", credential.PrincipalKey)
	}
	if len(strings.TrimPrefix(credential.PrincipalKey, "principal:")) != protocolPrincipalRandomBytes*2 {
		t.Fatalf("principal key has unexpected entropy length: %q", credential.PrincipalKey)
	}
}

func TestProtocolCredentialBeforeCreatePreservesExplicitOpaquePrincipal(t *testing.T) {
	credential := ProtocolCredential{
		SubscriptionID:     1,
		ProtocolEndpointID: 3,
		PrincipalKey:       "principal:existing-public-identity",
	}
	if err := credential.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate: %v", err)
	}
	if credential.PrincipalKey != "principal:existing-public-identity" {
		t.Fatalf("explicit opaque principal changed to %q", credential.PrincipalKey)
	}
}

func TestProtocolPrincipalKeysAreNonDeterministic(t *testing.T) {
	first, err := newProtocolPrincipalKey()
	if err != nil {
		t.Fatalf("first key: %v", err)
	}
	second, err := newProtocolPrincipalKey()
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if first == second {
		t.Fatalf("independent principal identities must differ: %q", first)
	}
}
