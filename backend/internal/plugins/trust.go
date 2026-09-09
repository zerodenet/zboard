package plugins

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type ImportPreview struct {
	Manifest      Manifest      `json:"manifest"`
	Digest        string        `json:"digest"`
	Publisher     string        `json:"publisher"`
	PublicKey     string        `json:"public_key"`
	Fingerprint   string        `json:"fingerprint"`
	Trusted       bool          `json:"trusted"`
	Compatibility Compatibility `json:"compatibility"`
}

func keyFingerprint(key string) string {
	raw, _ := base64.StdEncoding.DecodeString(key)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Inspection never writes trust, installs files, or starts a process. An
// embedded key proves package integrity, not the publisher's identity.
func (m *Manager) inspectPackage(data []byte, suppliedKey string) (*Package, error) {
	return readPackage(data, func(id string, sig Signature) (string, error) {
		if sig.KeyID == "" || len(sig.KeyID) > 160 || strings.ContainsAny(sig.KeyID, "\r\n") {
			return "", errors.New("invalid publisher ID")
		}
		if suppliedKey != "" {
			return strings.TrimSpace(suppliedKey), nil
		}
		if sig.PublicKey != "" {
			return sig.PublicKey, nil
		}
		var installed model.PluginInstallation
		if err := m.db.Where("id = ?", id).Limit(1).Find(&installed).Error; err != nil {
			return "", err
		}
		if installed.Publisher == sig.KeyID && installed.SigningKey != "" {
			return installed.SigningKey, nil
		}
		if key := m.options.TrustedPublishers[sig.KeyID]; key != "" {
			return key, nil
		}
		return "", errors.New("此旧版插件包未附带公钥，请在兼容选项中提供发布者的 .pub 公钥")
	})
}

// Once installed, even another trusted market cannot silently replace the key.
func (m *Manager) packageTrusted(p *Package) (bool, error) {
	var installed model.PluginInstallation
	if err := m.db.Where("id = ?", p.Manifest.ID).Limit(1).Find(&installed).Error; err != nil {
		return false, err
	}
	if installed.ID != "" {
		key := installed.SigningKey
		if key == "" {
			key = m.options.TrustedPublishers[installed.Publisher]
		}
		if installed.Publisher != p.Publisher || key == "" || key != p.PublicKey || (!installed.LocalTrust && m.options.TrustedPublishers[installed.Publisher] != key) {
			return false, errors.New("插件发布者或签名密钥已变化，禁止覆盖现有安装")
		}
		return true, nil
	}
	configured := m.options.TrustedPublishers[p.Publisher]
	if configured != "" && configured != p.PublicKey {
		return false, errors.New("publisher key conflicts with host configuration")
	}
	return configured != "", nil
}

func (m *Manager) PreviewImport(data []byte, publicKey string) (ImportPreview, error) {
	p, err := m.inspectPackage(data, publicKey)
	if err != nil {
		return ImportPreview{}, err
	}
	trusted, err := m.packageTrusted(p)
	if err != nil {
		return ImportPreview{}, err
	}
	if p.Manifest.Surfaces == nil {
		p.Manifest.Surfaces = []string{}
	}
	return ImportPreview{Manifest: p.Manifest, Digest: p.Digest, Publisher: p.Publisher, PublicKey: p.PublicKey, Fingerprint: keyFingerprint(p.PublicKey), Trusted: trusted, Compatibility: p.Manifest.Compatibility(m.host)}, nil
}
