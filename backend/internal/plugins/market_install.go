package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

func (m *Manager) inspectMarketPackage(ctx context.Context, id, version string) (MarketDetail, []byte, ImportPreview, error) {
	d, err := m.MarketDetail(ctx, id, version)
	if err != nil {
		return d, nil, ImportPreview{}, err
	}
	a, err := d.hostArtifact()
	if err != nil {
		return d, nil, ImportPreview{}, err
	}
	raw, err := m.fetch(ctx, a.URL, MaxPackageBytes)
	if err != nil {
		return d, nil, ImportPreview{}, err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), a.SHA256) || (a.Size > 0 && int64(len(raw)) != a.Size) {
		return d, nil, ImportPreview{}, errors.New("安装包摘要或大小与发行信息不符，请重新读取发行信息")
	}
	preview, err := m.PreviewImport(raw, d.publicKey)
	if err != nil {
		return d, nil, preview, err
	}
	if preview.Manifest.ID != id || preview.Manifest.Version != d.Release.Version || preview.Publisher != d.Entry.Publisher {
		return d, nil, preview, errors.New("安装包身份与市场条目不符")
	}
	if len(d.Entry.Surfaces) > 0 && !withinListingBoundary(preview.Manifest.Surfaces, d.Entry.Surfaces) {
		return d, nil, preview, errors.New("安装包界面范围超出市场准入上限")
	}
	if len(d.Entry.Capabilities) > 0 && !withinListingBoundary(preview.Manifest.Capabilities, d.Entry.Capabilities) {
		return d, nil, preview, errors.New("安装包能力超出市场准入上限")
	}
	if d.trusted {
		preview.Trusted = true
	}
	return d, raw, preview, nil
}
func (m *Manager) PreviewMarket(ctx context.Context, id, version string) (ImportPreview, error) {
	_, _, preview, err := m.inspectMarketPackage(ctx, id, version)
	return preview, err
}
func (m *Manager) InstallMarketConfirmed(ctx context.Context, id, version, digest, fingerprint, actor string) (Installation, error) {
	d, raw, preview, err := m.inspectMarketPackage(ctx, id, version)
	if err != nil {
		return Installation{}, err
	}
	if digest == "" || digest != preview.Digest {
		return Installation{}, ErrConflict
	}
	if d.trusted {
		p, err := m.inspectPackage(raw, d.publicKey)
		if err != nil {
			return Installation{}, err
		}
		return m.importVerified(raw, actor, p, true)
	}
	return m.ImportConfirmed(raw, actor, d.publicKey, digest, fingerprint)
}
