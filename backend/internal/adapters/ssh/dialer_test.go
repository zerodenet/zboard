package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

func TestValidateHostKeyFingerprint(t *testing.T) {
	valid := "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size))
	if err := ValidateHostKeyFingerprint(valid); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "MD5:abc", "SHA256:invalid"} {
		if err := ValidateHostKeyFingerprint(value); err == nil {
			t.Fatalf("ValidateHostKeyFingerprint(%q) accepted", value)
		}
	}
}

func TestVerifiedHostKeyCallbackCapturesAndChecksFingerprint(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := cryptossh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	want := cryptossh.FingerprintSHA256(publicKey)
	observed := ""
	if err := VerifiedHostKeyCallback(want, &observed)("node.example.com", nil, publicKey); err != nil {
		t.Fatal(err)
	}
	if observed != want {
		t.Fatalf("observed = %q, want %q", observed, want)
	}
	other := "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, sha256.Size))
	if err := VerifiedHostKeyCallback(other, nil)("node.example.com", nil, publicKey); err == nil {
		t.Fatal("changed host key accepted")
	}
}

func TestAuthMethodRejectsUnknownAndInvalidPrivateKey(t *testing.T) {
	if _, err := authMethod("keyboard-interactive", "secret", ""); err == nil {
		t.Fatal("unknown authentication method accepted")
	}
	if _, err := authMethod(AuthPrivateKey, "not-a-key", ""); err == nil {
		t.Fatal("invalid private key accepted")
	}
}
