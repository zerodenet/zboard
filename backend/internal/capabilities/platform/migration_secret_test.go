package platform

import (
	"encoding/json"
	"testing"
)

func TestMigrationSecretPreservesLegacyCiphertextAndClearsAsJSON(t *testing.T) {
	cipher := "zboard:v1:retained-cipher"
	for _, payload := range []string{cipher, EncodeMigrationSecret(cipher)} {
		decoded, err := DecodeMigrationSecret(payload)
		if err != nil || decoded != cipher {
			t.Fatal("ciphertext changed", err)
		}
		if !json.Valid([]byte(EncodeMigrationSecret(decoded))) {
			t.Fatal("storage is not JSON")
		}
	}
	for _, payload := range []string{"", "{}", "null"} {
		decoded, err := DecodeMigrationSecret(payload)
		if err != nil || decoded != "" || EncodeMigrationSecret(decoded) != "{}" {
			t.Fatal("cleared payload is not portable", err)
		}
	}
	for _, payload := range []string{`{"unknown":"do not discard"}`, `{"ciphertext":42}`, `{"ciphertext":"value"} {}`, `[]`, "invalid"} {
		if _, err := DecodeMigrationSecret(payload); err == nil {
			t.Fatal("unexpected migration payload accepted")
		}
	}
}
