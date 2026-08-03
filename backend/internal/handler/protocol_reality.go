package handler

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

type realityKeyPair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
}

type realityTemplateRequest struct {
	Preset string `json:"preset"`
}

type realityTemplate struct {
	Preset            string `json:"preset"`
	Label             string `json:"label"`
	ServerName        string `json:"server_name"`
	ClientFingerprint string `json:"client_fingerprint"`
	PrivateKey        string `json:"private_key"`
	PublicKey         string `json:"public_key"`
	ShortID           string `json:"short_id"`
}

type realityTemplatePreset struct {
	Label             string
	ServerName        string
	ClientFingerprint string
}

var realityTemplatePresets = map[string]realityTemplatePreset{
	"compatible": {Label: "通用兼容", ServerName: "www.microsoft.com", ClientFingerprint: "chrome"},
	"cdn":        {Label: "全球 CDN", ServerName: "www.cloudflare.com", ClientFingerprint: "chrome"},
	"apple":      {Label: "Apple 生态", ServerName: "www.apple.com", ClientFingerprint: "safari"},
}

func (h *handlers) ProtocolRealityKeyPairHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	pair, err := generateRealityKeyPair()
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pair)
}

func (h *handlers) ProtocolRealityTemplateHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var req realityTemplateRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, "invalid Reality template request")
		return
	}
	presetKey := strings.ToLower(strings.TrimSpace(req.Preset))
	if presetKey == "" {
		presetKey = "compatible"
	}
	preset, ok := realityTemplatePresets[presetKey]
	if !ok {
		BadRequest(w, "unknown Reality template preset")
		return
	}
	pair, err := generateRealityKeyPair()
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, realityTemplate{
		Preset: presetKey, Label: preset.Label, ServerName: preset.ServerName,
		ClientFingerprint: preset.ClientFingerprint, PrivateKey: pair.PrivateKey,
		PublicKey: pair.PublicKey, ShortID: pair.ShortID,
	})
}

func generateRealityKeyPair() (realityKeyPair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return realityKeyPair{}, err
	}
	shortID := make([]byte, 8)
	if _, err := rand.Read(shortID); err != nil {
		return realityKeyPair{}, err
	}
	if len(privateKey.Bytes()) != 32 {
		return realityKeyPair{}, errors.New("generated invalid X25519 private key")
	}
	return realityKeyPair{
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		ShortID:    hex.EncodeToString(shortID),
	}, nil
}

func validateVLESSRealityConfigs(server, client map[string]interface{}) error {
	serverReality, serverPresent := server["reality"].(map[string]interface{})
	clientReality, clientPresent := client["reality"].(map[string]interface{})
	if !serverPresent && !clientPresent {
		return nil
	}
	fields := map[string]string{}
	if !serverPresent {
		fields["config"] = "VLESS Reality 服务端配置必须包含 reality 对象。"
	}
	if !clientPresent {
		fields["client_config"] = "VLESS Reality 客户端配置必须包含 reality 对象。"
	}
	if len(fields) > 0 {
		return validationError("VLESS Reality 配置校验失败。", fields)
	}
	privateKey := strings.TrimSpace(stringValue(serverReality["private_key"]))
	privateBytes, privateErr := base64.RawURLEncoding.DecodeString(privateKey)
	if privateErr != nil || len(privateBytes) != 32 {
		fields["config"] = "Reality private_key 必须是 32 字节、无填充的 Base64URL。"
	}
	publicKey := strings.TrimSpace(stringValue(clientReality["public_key"]))
	publicBytes, publicErr := base64.RawURLEncoding.DecodeString(publicKey)
	if publicErr != nil || len(publicBytes) != 32 {
		fields["client_config"] = "Reality public_key 必须是 32 字节、无填充的 Base64URL。"
	}
	if privateErr == nil && len(privateBytes) == 32 && publicErr == nil && len(publicBytes) == 32 {
		key, err := ecdh.X25519().NewPrivateKey(privateBytes)
		if err != nil || !strings.EqualFold(base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), publicKey) {
			fields["client_config"] = "Reality public_key 与服务端 private_key 不匹配。"
		}
	}
	shortIDs, ok := serverReality["short_ids"].([]interface{})
	if !ok || len(shortIDs) == 0 {
		fields["config"] = "Reality 服务端至少需要一个 short_id。"
	} else {
		for _, raw := range shortIDs {
			if !validRealityShortID(stringValue(raw)) {
				fields["config"] = "Reality short_ids 必须是 2–16 位偶数长度十六进制字符串。"
				break
			}
		}
	}
	clientShortID := strings.TrimSpace(stringValue(clientReality["short_id"]))
	if !validRealityShortID(clientShortID) {
		fields["client_config"] = "Reality short_id 必须是 2–16 位偶数长度十六进制字符串。"
	} else if ok && len(shortIDs) > 0 {
		matched := false
		for _, raw := range shortIDs {
			matched = matched || strings.EqualFold(clientShortID, strings.TrimSpace(stringValue(raw)))
		}
		if !matched {
			fields["client_config"] = "Reality 客户端 short_id 必须存在于服务端 short_ids 中。"
		}
	}
	if strings.TrimSpace(stringValue(serverReality["server_name"])) == "" {
		fields["config"] = "Reality 服务端必须设置 server_name。"
	}
	if strings.TrimSpace(stringValue(clientReality["server_name"])) == "" {
		fields["client_config"] = "Reality 客户端必须设置 server_name。"
	}
	if len(fields) > 0 {
		return validationError("VLESS Reality 配置校验失败。", fields)
	}
	return nil
}

func validRealityShortID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || len(value) > 16 || len(value)%2 != 0 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
