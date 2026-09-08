package plugins

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const StorageCapability = "zboard.storage.v1"
const ConfigCapability = "zboard.config.v1"
const PageCapability = "zboard.ui.page.v1"

var ErrPermission = errors.New("plugin capability has not been authorized for this package")

type Authorization struct {
	Reviewed      bool     `json:"reviewed"`
	Granted       []string `json:"granted"`
	NativeTrusted bool     `json:"native_trusted"`
}

func hasCapability(v Installation, capability string) bool {
	return v.Authorization.Reviewed && slices.Contains(v.Authorization.Granted, capability) && slices.Contains(v.Manifest.Capabilities, capability)
}
func (m *Manager) loadAuthorization(v *Installation) error {
	v.Authorization.Granted = []string{}
	var row model.PluginAuthorization
	result := m.db.Where("plugin_id = ?", v.ID).Limit(1).Find(&row)
	err := result.Error
	if err == nil && result.RowsAffected == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(row.Capabilities), &v.Authorization.Granted); err != nil {
		return err
	}
	v.Authorization.Reviewed = row.Digest == v.Digest
	v.Authorization.NativeTrusted = row.NativeTrusted
	return nil
}
func (m *Manager) Authorize(id, actor, digest string, generation uint64, capabilities []string, nativeTrusted bool) (Installation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, err := m.load(id)
	if err != nil {
		return v, err
	}
	if err := m.guard(m.db); err != nil {
		return v, err
	}
	if generation != v.Generation || digest != v.Digest {
		return v, ErrConflict
	}
	if v.State == "uninstalled" {
		return v, ErrUnavailable
	}
	seen := map[string]bool{}
	for _, c := range capabilities {
		if seen[c] || !slices.Contains(v.Manifest.Capabilities, c) {
			return v, ErrPermission
		}
		seen[c] = true
	}
	if nativeTrusted && v.Manifest.Components.Server == nil {
		return v, ErrPermission
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	raw, _ := json.Marshal(capabilities)
	op, err := m.newOperation(id, "authorize", actor)
	if err != nil {
		return v, err
	}
	err = m.db.Transaction(func(tx *gorm.DB) error {
		if err := m.guard(tx); err != nil {
			return err
		}
		row := model.PluginAuthorization{PluginID: id, Digest: digest, Capabilities: string(raw), NativeTrusted: nativeTrusted, Actor: actor}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Model(&model.PluginInstallation{}).Where("id = ? AND generation = ?", id, generation).Updates(map[string]any{"enabled": false, "state": "disabled", "last_error": "", "generation": gorm.Expr("generation + 1")}).Error
	})
	if err == nil {
		m.processes[id].close()
		delete(m.processes, id)
		m.invalidate(id)
	}
	if err := m.finish(op, err); err != nil {
		return v, err
	}
	return m.load(id)
}
func executionAuthorized(v Installation) error {
	if !v.Authorization.Reviewed {
		return ErrPermission
	}
	if v.Manifest.Components.Server != nil && (!v.Authorization.NativeTrusted || !hasCapability(v, ConfigCapability)) {
		return ErrPermission
	}
	return nil
}
