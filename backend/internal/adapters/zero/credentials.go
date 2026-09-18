package zero

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type Issuer struct {
	Cipher Cipher
	Mieru  bool
}

func (i Issuer) Supports(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "shadowsocks", "trojan", "hysteria2":
		return true
	case "mieru":
		return i.Mieru
	default:
		return false
	}
}
func (i Issuer) Status(protocol string, principalReady bool) string {
	if strings.EqualFold(strings.TrimSpace(protocol), "mieru") && !i.Mieru && !principalReady {
		return "prepared"
	}
	return "active"
}
func (i Issuer) Encrypt(secret string) (string, error) { return i.Cipher.Encrypt(secret) }

func (i Issuer) Secret(protocol, serverConfig string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess":
		return uuid.NewString(), nil
	case "shadowsocks":
		keyBytes := 32
		rawTemplate, err := i.Cipher.Decrypt(serverConfig)
		if err != nil {
			return "", err
		}
		var server map[string]interface{}
		if err := json.Unmarshal([]byte(rawTemplate), &server); err != nil {
			return "", err
		}
		cipherName, _ := server["cipher"].(string)
		cipherName = strings.ToLower(strings.TrimSpace(cipherName))
		if strings.Contains(cipherName, "2022") && strings.Contains(cipherName, "aes-128") {
			keyBytes = 16
		}
		entropy := make([]byte, keyBytes)
		if _, err := rand.Read(entropy); err != nil {
			return "", err
		}
		if strings.Contains(cipherName, "2022") {
			return base64.StdEncoding.EncodeToString(entropy), nil
		}
		return base64.RawURLEncoding.EncodeToString(entropy), nil
	case "trojan", "hysteria2", "mieru":
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(entropy), nil
	default:
		return "", fmt.Errorf("protocol %s does not support subscription credentials", protocol)
	}
}
