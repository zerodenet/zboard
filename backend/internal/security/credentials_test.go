package security

import (
	"strings"
	"testing"
)

const testCredentialKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCredentialCipherRoundTrip(t *testing.T) {
	cipher, err := NewCredentialCipher(testCredentialKey)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	encrypted, err := cipher.Encrypt("ssh-password")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if !IsEncryptedCredential(encrypted) || strings.Contains(encrypted, "ssh-password") {
		t.Fatalf("Encrypt() = %q, want versioned ciphertext", encrypted)
	}
	plaintext, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "ssh-password" {
		t.Fatalf("Decrypt() = %q, want original password", plaintext)
	}
}

func TestCredentialCipherRejectsWrongKeyAndPlaintext(t *testing.T) {
	first, _ := NewCredentialCipher(testCredentialKey)
	second, _ := NewCredentialCipher("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	encrypted, _ := first.Encrypt("ssh-password")
	if _, err := second.Decrypt(encrypted); err == nil {
		t.Fatal("Decrypt() error = nil, want wrong-key rejection")
	}
	if _, err := first.Decrypt("ssh-password"); err == nil {
		t.Fatal("Decrypt() error = nil, want plaintext rejection")
	}
}

func TestCredentialKeyValidation(t *testing.T) {
	if err := ValidateCredentialKey(testCredentialKey); err != nil {
		t.Fatalf("ValidateCredentialKey() error = %v", err)
	}
	for _, value := range []string{"", "short", strings.Repeat("a", 62)} {
		if err := ValidateCredentialKey(value); err == nil {
			t.Fatalf("ValidateCredentialKey(%q) error = nil, want rejection", value)
		}
	}
}
