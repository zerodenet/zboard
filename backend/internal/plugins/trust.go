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
	Manifest           Manifest            `json:"manifest"`
	Digest             string              `json:"digest"`
	Publisher          string              `json:"publisher"`
	PublicKey          string              `json:"public_key"`
	Fingerprint        string              `json:"fingerprint"`
	Trusted            bool                `json:"trusted"`
	Compatibility      Compatibility       `json:"compatibility"`
	PublisherMigration *PublisherMigration `json:"publisher_migration,omitempty"`
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
func (m *Manager) assessPackage(p *Package) (bool, model.PluginInstallation, error) {
	var installed model.PluginInstallation
	if err := m.db.Where("id = ?", p.Manifest.ID).Limit(1).Find(&installed).Error; err != nil {
		return false, installed, err
	}
	configured := m.options.TrustedPublishers[p.Publisher]
	if configured != "" && configured != p.PublicKey {
		return false, installed, errors.New("publisher key conflicts with host configuration")
	}
	if installed.ID != "" {
		key := installed.SigningKey
		if key == "" {
			key = m.options.TrustedPublishers[installed.Publisher]
		}
		oldConfigured := m.options.TrustedPublishers[installed.Publisher]
		if key == "" || key != p.PublicKey || (oldConfigured != "" && oldConfigured != key) || (!installed.LocalTrust && oldConfigured != key) {
			return false, installed, errors.New("插件签名密钥已变化或信任已撤销，禁止覆盖现有安装")
		}
		if installed.Publisher != p.Publisher && installed.SigningKey == "" {
			return false, installed, errors.New("迁移发布者前必须存在已固定的插件签名密钥")
		}
		return true, installed, nil
	}
	return configured != "", installed, nil
}

func (m *Manager) PreviewImport(data []byte, publicKey string) (ImportPreview, error) {
	p, err := m.inspectPackage(data, publicKey)
	if err != nil {
		return ImportPreview{}, err
	}
	trusted, installed, err := m.assessPackage(p)
	if err != nil {
		return ImportPreview{}, err
	}
	if p.Manifest.Surfaces == nil {
		p.Manifest.Surfaces = []string{}
	}
	return ImportPreview{Manifest: p.Manifest, Digest: p.Digest, Publisher: p.Publisher, PublicKey: p.PublicKey, Fingerprint: keyFingerprint(p.PublicKey), Trusted: trusted, Compatibility: p.Manifest.Compatibility(m.host), PublisherMigration: publisherMigration(installed, p)}, nil
}
