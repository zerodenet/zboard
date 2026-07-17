package datastore

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/security"
)

func TestSecureNodeCredentialMigratesPlaintextAndValidatesCiphertext(t *testing.T) {
	cipher, err := security.NewCredentialCipher("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	migrated, changed, err := secureNodeCredential("legacy-password", cipher)
	if err != nil {
		t.Fatalf("secureNodeCredential() error = %v", err)
	}
	if !changed || !security.IsEncryptedCredential(migrated) {
		t.Fatalf("secureNodeCredential() = %q, %v, want migrated ciphertext", migrated, changed)
	}
	validated, changed, err := secureNodeCredential(migrated, cipher)
	if err != nil {
		t.Fatalf("secureNodeCredential(ciphertext) error = %v", err)
	}
	if changed || validated != migrated {
		t.Fatal("existing ciphertext should only be validated")
	}
}
