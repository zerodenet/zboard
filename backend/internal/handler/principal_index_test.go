package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestPurchasedCredentialPrincipalIndexMatchesCanonicalMaterial(t *testing.T) {
	f := newOrderFixture(t)
	_ = attachOrderPublishEndpoint(t, f)
	paid := f.paid(t, f.create(t, 0).ID)
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", paid.SubscriptionID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := f.h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if credential.PrincipalLookupVersion != 1 || credential.PrincipalDigest != entitlements.RetiredPrincipalHash(credential.NodeID, credential.PrincipalKey) || credential.SecretPrincipalDigest != entitlements.RetiredPrincipalHash(credential.NodeID, secret) {
		t.Fatalf("purchase index mismatch: version=%d principal_match=%v secret_match=%v", credential.PrincipalLookupVersion, credential.PrincipalDigest == entitlements.RetiredPrincipalHash(credential.NodeID, credential.PrincipalKey), credential.SecretPrincipalDigest == entitlements.RetiredPrincipalHash(credential.NodeID, secret))
	}
	for _, provided := range []string{credential.PrincipalKey, secret} {
		result, err := (credentialstore.PrincipalIndex{DB: f.h.db, Cipher: f.h.credentialCipher}).Lookup(context.Background(), credential.NodeID, provided)
		if err != nil || result.ID != credential.ID {
			t.Fatalf("purchased principal lookup failed: id=%d expected=%d error=%v", result.ID, credential.ID, err)
		}
	}
}
