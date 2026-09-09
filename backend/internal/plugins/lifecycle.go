package plugins

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Caller holds the host lifecycle lock. Candidate processes cannot use host
// services until the package, configuration, data and receipt commit together.
func (m *Manager) commitCandidate(ctx context.Context, prev model.PluginInstallation, pack *Package, actor string, op *model.PluginOperation) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	candidate := Installation{PluginInstallation: prev, Manifest: pack.Manifest, Digest: pack.Digest, Version: pack.Manifest.Version, Compatibility: pack.Manifest.Compatibility(m.host)}
	candidate.ID, candidate.Name, candidate.Publisher = pack.Manifest.ID, pack.Manifest.Name, pack.Publisher
	candidate.LocalTrust = pack.LocalTrust
	candidate.SigningKey = pack.PublicKey
	candidate.VersionID = pack.Digest
	candidate.Generation = prev.Generation + 1
	candidate.Enabled = prev.Enabled && prev.State != "uninstalled"
	candidate.State, candidate.LastError = "disabled", ""
	if !candidate.Compatibility.Compatible {
		return errors.New("plugin is incompatible with this host")
	}
	var err error
	candidate.Admission, err = hostAdmission(pack.Manifest)
	if err != nil {
		return err
	}
	if err := m.loadDataStatus(&candidate); err != nil {
		return err
	}
	plan, err := m.planMigration(candidate, actor)
	if err != nil {
		return err
	}
	var proc *process
	// Fresh, unconfigured installs may need operator parameters before startup.
	// Existing or migrated configuration must pass the candidate runtime first.
	if pack.Manifest.Components.Server != nil && (candidate.Enabled || prev.ConfigRevision > 0 || plan.row != nil) {
		proc, err = m.startAuthorizedProcess(ctx, candidate)
		if err != nil {
			return err
		}
		defer func() {
			if proc != nil {
				proc.close()
			}
		}()
		normalized, err := proc.api.ValidateConfig(ctx, &pluginv1.ConfigRequest{ConfigJson: plan.config, Revision: candidate.ConfigRevision + 1})
		if err != nil || normalized == nil || validConfig(normalized.NormalizedJson) != nil {
			return errors.New("candidate plugin rejected configuration")
		}
		if string(normalized.NormalizedJson) != string(plan.config) {
			plan.configChanged = true
		}
		plan.config = normalized.NormalizedJson
		revision := candidate.ConfigRevision
		if plan.configChanged {
			revision++
		}
		result, err := proc.api.ApplyConfig(ctx, &pluginv1.ConfigRequest{ConfigJson: plan.config, Revision: revision})
		if err != nil || result == nil || !result.Healthy {
			return errors.New("candidate plugin could not apply configuration")
		}
	}
	if plan.configChanged {
		candidate.ConfigCiphertext, err = m.cipher.Encrypt(string(plan.config))
		if err != nil {
			return err
		}
		candidate.ConfigRevision++
	}
	if candidate.Enabled {
		candidate.State = "active"
	}
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		version := model.PluginVersion{ID: pack.Digest, PluginID: candidate.ID, Version: candidate.Version, Digest: pack.Digest, Publisher: pack.Publisher, Manifest: string(pack.RawManifest)}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&version).Error; err != nil {
			return err
		}
		if err := plan.commit(tx); err != nil {
			return err
		}
		if err := storeAdmission(tx, candidate, actor); err != nil {
			return err
		}
		if err := tx.Save(&candidate.PluginInstallation).Error; err != nil {
			return err
		}
		if op != nil {
			return tx.Model(op).Updates(map[string]any{"state": "succeeded", "message": ""}).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	old := m.processes[candidate.ID]
	delete(m.processes, candidate.ID)
	if candidate.Enabled && proc != nil {
		m.processes[candidate.ID] = proc
		proc = nil
	}
	old.close()
	m.invalidate(candidate.ID)
	return nil
}
