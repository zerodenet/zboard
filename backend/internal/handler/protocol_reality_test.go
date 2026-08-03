package handler

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVLESSRealityConfigs(t *testing.T) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateValue := base64.RawURLEncoding.EncodeToString(privateKey.Bytes())
	publicValue := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	server := map[string]interface{}{"type": "vless", "reality": map[string]interface{}{
		"private_key": privateValue, "short_ids": []interface{}{"0123456789abcdef"}, "server_name": "edge.example.com",
	}}
	client := map[string]interface{}{"type": "vless", "reality": map[string]interface{}{
		"public_key": publicValue, "short_id": "0123456789abcdef", "server_name": "edge.example.com", "client_fingerprint": "chrome",
	}}
	if err := validateVLESSRealityConfigs(server, client); err != nil {
		t.Fatalf("validateVLESSRealityConfigs() error = %v", err)
	}
	client["reality"].(map[string]interface{})["short_id"] = "not-hex"
	if err := validateVLESSRealityConfigs(server, client); err == nil {
		t.Fatal("validateVLESSRealityConfigs() accepted an invalid short ID")
	}
}

func TestGenerateRealityTemplateInputs(t *testing.T) {
	for name, preset := range realityTemplatePresets {
		if name == "" || preset.Label == "" || preset.ServerName == "" || preset.ClientFingerprint == "" {
			t.Fatalf("invalid Reality preset %q: %+v", name, preset)
		}
	}
	pair, err := generateRealityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.PrivateKey) != 43 || len(pair.PublicKey) != 43 || len(pair.ShortID) != 16 {
		t.Fatalf("unexpected generated Reality values: private=%d public=%d short=%d", len(pair.PrivateKey), len(pair.PublicKey), len(pair.ShortID))
	}
}

func TestCurrentZeroAcceptsVLESSRealityEndpoint(t *testing.T) {
	validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"))
	if validator == "" {
		t.Skip("ZBOARD_ZERO_VALIDATE_BIN is not configured")
	}
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]interface{}{
		"inbounds": []interface{}{map[string]interface{}{
			"tag": "reality", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 24443},
			"protocol": map[string]interface{}{
				"type": "vless", "users": []interface{}{map[string]interface{}{"id": "11111111-1111-4111-8111-111111111111"}},
				"reality": map[string]interface{}{
					"private_key": base64.RawURLEncoding.EncodeToString(privateKey.Bytes()),
					"short_ids":   []interface{}{"0123456789abcdef"}, "server_name": "edge.example.com",
				},
			},
		}},
		"mode":  map[string]interface{}{"type": "rule"},
		"route": map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}},
	}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "vless-reality.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(validator, "validate", path).CombinedOutput(); err != nil {
		t.Fatalf("zero validate VLESS Reality failed: %v\n%s", err, output)
	}
}
