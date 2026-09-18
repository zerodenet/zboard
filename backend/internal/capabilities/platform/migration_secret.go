package platform

import (
	"encoding/json"
	"errors"
	"strings"
)

// MigrationSecret stores only ciphertext in the JSON task payload column.
type MigrationSecret struct {
	Ciphertext string `json:"ciphertext"`
}

func EncodeMigrationSecret(ciphertext string) string {
	if ciphertext == "" {
		return "{}"
	}
	payload, _ := json.Marshal(MigrationSecret{Ciphertext: ciphertext})
	return string(payload)
}
func DecodeMigrationSecret(payload string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return "", nil
	}
	// Legacy SQLite tasks stored the host cipher envelope without JSON wrapping.
	if strings.HasPrefix(payload, "zboard:v1:") {
		return payload, nil
	}
	var value MigrationSecret
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if !json.Valid([]byte(payload)) {
		return "", errors.New("invalid database migration secret payload")
	}
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("invalid database migration secret payload")
	}
	return value.Ciphertext, nil
}
