package model

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
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

func TestProtocolPrincipalDigestPreservesRetiredV1AndNormalizedIdentity(t *testing.T) {
	if got := entitlements.RetiredPrincipalHash(7, "pre-index-retired-secret"); got != "9bc0f311efb3589154cbfc913a530b3a964bc8f23fe3cd9d708ee9aa77ab9775" {
		t.Fatal("retired v1 digest compatibility changed")
	}
	credential := ProtocolCredential{NodeID: 7, SubscriptionID: 1, ProtocolEndpointID: 3, PrincipalKey: "subscription:1:endpoint:3", PrincipalLookupVersion: 1}
	if err := credential.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if credential.PrincipalKey == "subscription:1:endpoint:3" || credential.PrincipalDigest != entitlements.RetiredPrincipalHash(7, credential.PrincipalKey) {
		t.Fatal("lookup projection precedes identity normalization")
	}
}
