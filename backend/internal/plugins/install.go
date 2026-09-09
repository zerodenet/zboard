package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func (m *Manager) Import(data []byte, actor string) (Installation, error) {
	p, err := ReadPackage(data, m.options.TrustedPublishers)
	if err != nil {
		return Installation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db); err != nil {
		return Installation{}, err
	}
	var prev model.PluginInstallation
	if err := m.db.Where("id = ?", p.Manifest.ID).Limit(1).Find(&prev).Error; err != nil {
		return Installation{}, err
	}
	if prev.ID != "" && prev.Publisher != p.Publisher {
		return Installation{}, errors.New("publisher must match")
	}
	if !p.Manifest.Compatibility(m.host).Compatible {
		return Installation{}, errors.New("plugin is incompatible with this host")
	}
	if prev.ID == "" {
		var count int64
		if err := m.db.Model(&model.PluginInstallation{}).Count(&count).Error; err != nil {
			return Installation{}, err
		}
		if count >= 200 {
			return Installation{}, errors.New("plugin installation limit reached")
		}
	}
	var count int64
	if err := m.db.Model(&model.PluginVersion{}).Where("plugin_id = ? AND id <> ?", p.Manifest.ID, p.Digest).Count(&count).Error; err != nil {
		return Installation{}, err
	}
	if count >= 30 {
		return Installation{}, errors.New("plugin version history limit reached (30)")
	}
	// An active installation cannot implicitly accept an untested runtime upgrade.
	if prev.Enabled && !p.Manifest.Compatibility(m.host).Tested {
		return Installation{}, errors.New("disable before importing an untested host version")
	}
	op, err := m.newOperation(p.Manifest.ID, "import", actor)
	if err != nil {
		return Installation{}, err
	}
	target := filepath.Join(m.options.Directory, "versions", p.Digest)
	_, before := os.Lstat(target)
	err = writePackage(m.options.Directory, p, data)
	if err == nil {
		err = m.commitCandidate(context.Background(), prev, p, actor, &op)
	}
	if err != nil {
		if os.IsNotExist(before) {
			_ = os.RemoveAll(target)
		}
		return Installation{}, m.finish(op, err)
	}
	return m.load(p.Manifest.ID)
}
func (m *Manager) packageFor(v Installation) (*Package, error) {
	if !digestPattern.MatchString(v.Digest) {
		return nil, errors.New("invalid stored package digest")
	}
	data, err := os.ReadFile(filepath.Join(m.options.Directory, "versions", v.Digest, "package.zbplugin"))
	if err != nil {
		return nil, errors.New("plugin package missing; import it again")
	}
	p, err := ReadPackage(data, m.options.TrustedPublishers)
	if err != nil {
		return nil, err
	}
	if p.Digest != v.Digest || p.Manifest.ID != v.ID || p.Publisher != v.Publisher {
		return nil, errors.New("stored package identity mismatch")
	}
	return p, nil
}
func (m *Manager) prepare(ctx context.Context, v Installation) (*process, error) {
	if err := executionAuthorized(v); err != nil {
		return nil, err
	}
	if err := m.checkDataCompatibility(v); err != nil {
		return nil, err
	}
	if !v.Compatibility.Compatible {
		return nil, errors.New("plugin is incompatible")
	}
	p, err := m.packageFor(v)
	if err != nil {
		return nil, err
	}
	if p.Manifest.Components.Server == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proc, err := m.startAuthorizedProcess(ctx, v)
	if err != nil {
		return nil, err
	}
	raw, err := m.config(v)
	if err == nil {
		_, err = proc.apply(ctx, raw, v.ConfigRevision)
	}
	if err != nil {
		proc.close()
		return nil, err
	}
	return proc, nil
}
func (m *Manager) Action(ctx context.Context, id, action, actor string, generation uint64, acceptUntested bool, versionID string) (Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.guard(m.db); err != nil {
		return Installation{}, err
	}
	v, err := m.load(id)
	if err != nil {
		return v, err
	}
	if generation != v.Generation {
		return v, ErrConflict
	}
	if action != "enable" && action != "disable" && action != "uninstall" && action != "rollback" && action != "purge_data" {
		return v, errors.New("unsupported plugin operation")
	}
	if action == "enable" && (!v.Compatibility.Compatible || (!v.Compatibility.Tested && !acceptUntested)) {
		return v, errors.New("incompatible or unconfirmed host version")
	}
	if (action == "rollback" || action == "purge_data") && v.Enabled {
		return v, errors.New("disable plugin first")
	}
	if action == "enable" {
		if err := executionAuthorized(v); err != nil {
			return v, err
		}
		if err := m.checkDataCompatibility(v); err != nil {
			return v, err
		}
	}
	if action == "enable" && v.State == "uninstalled" {
		return v, errors.New("import plugin before enabling")
	}
	if action == "purge_data" && v.State != "uninstalled" {
		return v, errors.New("uninstall before deleting configuration")
	}
	if action == "enable" && v.Enabled && v.State == "active" && (v.Manifest.Components.Server == nil || (m.processes[id] != nil && !m.processes[id].client.Exited())) {
		return v, nil
	}
	op, err := m.newOperation(id, action, actor)
	if err != nil {
		return v, err
	}
	switch action {
	case "purge_data":
		err = m.purgeDataLocked(v)
	case "enable":
		if old := m.processes[id]; old != nil {
			old.close()
			delete(m.processes, id)
		}
		var p *process
		p, err = m.prepare(ctx, v)
		if err == nil {
			err = m.db.Transaction(func(tx *gorm.DB) error {
				if err := m.guard(tx); err != nil {
					return err
				}
				return tx.Model(&model.PluginInstallation{}).Where("id = ? AND generation = ?", id, generation).Updates(map[string]any{"enabled": true, "state": "active", "last_error": "", "generation": gorm.Expr("generation + 1")}).Error
			})
		}
		if err == nil {
			if p != nil {
				m.processes[id] = p
			}
		} else {
			p.close()
			_ = m.updateInstallation(id, map[string]any{"state": "failed"})
		}
	case "disable":
		err = m.updateInstallation(id, map[string]any{"enabled": false, "state": "disabled", "generation": gorm.Expr("generation + 1")})
		if err == nil {
			m.processes[id].close()
			delete(m.processes, id)
		}
	case "uninstall":
		err = m.updateInstallation(id, map[string]any{"enabled": false, "state": "uninstalled", "generation": gorm.Expr("generation + 1")})
		if err == nil {
			m.processes[id].close()
			delete(m.processes, id)
			for _, ver := range v.Versions {
				if digestPattern.MatchString(ver.Digest) {
					err = errors.Join(err, os.RemoveAll(filepath.Join(m.options.Directory, "versions", ver.Digest)))
				}
			}
		}
	case "rollback":
		var ver model.PluginVersion
		err = m.db.First(&ver, "id = ? AND plugin_id = ?", versionID, id).Error
		if err == nil {
			candidate := v
			candidate.Digest, candidate.Version = ver.Digest, ver.Version
			var pack *Package
			pack, err = m.packageFor(candidate)
			if err == nil {
				err = m.commitCandidate(ctx, v.PluginInstallation, pack, actor, nil)
			}
		}
	}
	m.invalidate(id)
	if err != nil {
		_ = m.updateInstallation(id, map[string]any{"last_error": err.Error()})
	}
	if err = m.finish(op, err); err != nil {
		return v, err
	}
	return m.load(id)
}
